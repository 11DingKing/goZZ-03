package portapp

import (
	"fmt"
	"sort"
	"time"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

// Service implements 进港预约 (port-arrival appointment) and the re-scheduling
// logic triggered by ship delays and berth-plan changes.
type Service struct {
	store *store.Store
	clock clock.Clock
}

// New creates a port-appointment service.
func New(s *store.Store, c clock.Clock) *Service {
	return &Service{store: s, clock: c}
}

// CreateAppointmentReq schedules a truck pickup for a confirmed booking.
type CreateAppointmentReq struct {
	BookingID   string
	TruckID     string
	WindowStart time.Time
	WindowEnd   time.Time
}

// CreateAppointment schedules a truck pickup (进港预约) for a confirmed or
// manifested booking. At most one active appointment exists per booking; a
// repeat call returns the existing one (idempotent).
func (s *Service) CreateAppointment(req CreateAppointmentReq) (*domain.PortAppointment, error) {
	if req.WindowEnd.Before(req.WindowStart) {
		return nil, fmt.Errorf("%w: window end before start", domain.ErrInvalidState)
	}
	var apt *domain.PortAppointment
	err := s.store.Update(func(d *store.Data) error {
		bk := d.Bookings[req.BookingID]
		if bk == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, req.BookingID)
		}
		if bk.Status != domain.BookingConfirmed && bk.Status != domain.BookingManifested {
			return fmt.Errorf("%w: booking %s status %s cannot appoint", domain.ErrInvalidState, req.BookingID, bk.Status)
		}
		for _, a := range d.Appointments {
			if a.BookingID == req.BookingID && (a.Status == domain.AptScheduled || a.Status == domain.AptRescheduled) {
				apt = a
				return nil
			}
		}
		id := store.NextID(d, "AP")
		now := s.clock.Now()
		na := &domain.PortAppointment{
			ID: id, BookingID: req.BookingID, VoyageID: bk.VoyageID,
			TruckID: req.TruckID, WindowStart: req.WindowStart, WindowEnd: req.WindowEnd,
			Status: domain.AptScheduled, Priority: bk.Priority, CreatedAt: now,
		}
		na.Sequence = nextSequence(d, bk.VoyageID)
		d.Appointments[id] = na
		apt = na
		return nil
	})
	return apt, err
}

// CompleteAppointment marks an appointment completed (truck arrived at port).
func (s *Service) CompleteAppointment(id string) (*domain.PortAppointment, error) {
	var apt *domain.PortAppointment
	err := s.store.Update(func(d *store.Data) error {
		a := d.Appointments[id]
		if a == nil {
			return fmt.Errorf("%w: appointment %s", domain.ErrNotFound, id)
		}
		if a.Status != domain.AptScheduled && a.Status != domain.AptRescheduled {
			return fmt.Errorf("%w: appointment %s status %s cannot complete", domain.ErrInvalidState, id, a.Status)
		}
		a.Status = domain.AptCompleted
		apt = a
		return nil
	})
	return apt, err
}

// Get returns an appointment by id.
func (s *Service) Get(id string) (*domain.PortAppointment, bool) {
	var a *domain.PortAppointment
	s.store.View(func(d *store.Data) { a = d.Appointments[id] })
	return a, a != nil
}

// RescheduleForVoyage re-appoints all trucks for a voyage, re-allocated by
// original priority (peak-season new-energy first, then by booking submit time).
// This is the automatic re-appointment triggered when a ship is delayed.
func (s *Service) RescheduleForVoyage(voyageID string) []string {
	var ids []string
	s.store.Update(func(d *store.Data) error {
		ids = s.rescheduleLocked(d, voyageID, true)
		return nil
	})
	return ids
}

// ResequenceByPriority reorders appointment sequences so that peak-season
// new-energy orders are prioritised and regular cargo is postponed. Used when
// the berth plan changes.
func (s *Service) ResequenceByPriority(voyageID string) []string {
	var ids []string
	s.store.Update(func(d *store.Data) error {
		var apts []*domain.PortAppointment
		for _, a := range d.Appointments {
			if a.VoyageID == voyageID && (a.Status == domain.AptScheduled || a.Status == domain.AptRescheduled) {
				apts = append(apts, a)
			}
		}
		orderByPriority(d, apts)
		for seq, a := range apts {
			a.Sequence = seq + 1
			ids = append(ids, a.ID)
		}
		return nil
	})
	return ids
}

// Reconcile re-schedules appointments for voyages still marked delayed. It only
// touches appointments left in the scheduled state (those not yet re-processed
// when a crash interrupted a delay). This is the startup recovery path.
func (s *Service) Reconcile() {
	s.store.Update(func(d *store.Data) error {
		for _, v := range d.Voyages {
			if v.Status == domain.VoyageDelayed {
				s.rescheduleLocked(d, v.ID, false)
			}
		}
		return nil
	})
}

// rescheduleLocked re-appoints trucks for a voyage by priority. When
// rescheduledToo is true, appointments already re-scheduled are re-handled
// (a fresh delay); otherwise only scheduled appointments are processed (startup
// recovery). Caller must hold the write lock.
func (s *Service) rescheduleLocked(d *store.Data, voyageID string, rescheduledToo bool) []string {
	var apts []*domain.PortAppointment
	for _, a := range d.Appointments {
		if a.VoyageID != voyageID {
			continue
		}
		if a.Status == domain.AptScheduled || (rescheduledToo && a.Status == domain.AptRescheduled) {
			a.Status = domain.AptReScheduling
			apts = append(apts, a)
		}
	}
	orderByPriority(d, apts)
	now := s.clock.Now()
	var ids []string
	for seq, a := range apts {
		a.Sequence = seq + 1
		if shift := now.Sub(a.WindowStart); shift > 0 {
			a.WindowStart = a.WindowStart.Add(shift)
			a.WindowEnd = a.WindowEnd.Add(shift)
		}
		a.Status = domain.AptRescheduled
		ids = append(ids, a.ID)
	}
	return ids
}

// orderByPriority sorts appointments by booking priority desc, then by original
// booking submit time asc (original arrival order), then by id for stability.
func orderByPriority(d *store.Data, apts []*domain.PortAppointment) {
	sort.SliceStable(apts, func(i, j int) bool {
		if apts[i].Priority != apts[j].Priority {
			return apts[i].Priority > apts[j].Priority
		}
		bi, bj := d.Bookings[apts[i].BookingID], d.Bookings[apts[j].BookingID]
		if bi != nil && bj != nil {
			if !bi.SubmitTime.Equal(bj.SubmitTime) {
				return bi.SubmitTime.Before(bj.SubmitTime)
			}
		}
		return apts[i].ID < apts[j].ID
	})
}

func nextSequence(d *store.Data, voyageID string) int {
	max := 0
	for _, a := range d.Appointments {
		if a.VoyageID == voyageID && a.Sequence > max {
			max = a.Sequence
		}
	}
	return max + 1
}
