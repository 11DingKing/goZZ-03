package dispatch

import (
	"fmt"
	"time"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/store"
)

// Service implements voyage lifecycle, slot provisioning, ship-delay handling,
// berth-plan changes and dual-signature port changes. It is the 航线调度专员
// (route dispatch specialist) surface.
type Service struct {
	store   *store.Store
	clock   clock.Clock
	portapp *portapp.Service
}

// New creates a dispatch service. The port-appointment service is injected so a
// delay can auto-trigger re-appointment without creating an import cycle.
func New(s *store.Store, c clock.Clock, pa *portapp.Service) *Service {
	return &Service{store: s, clock: c, portapp: pa}
}

// CreateVoyageReq carries the data to provision a voyage and its slots.
type CreateVoyageReq struct {
	ID                string
	Route             string
	OriginPort        string
	DestPort          string
	Berth             string
	DepartureAt       time.Time
	ArrivalAt         time.Time
	RefrigeratedSlots int
	DrySlots          int
}

// CreateVoyage provisions a voyage with the requested slot mix.
func (s *Service) CreateVoyage(req CreateVoyageReq) (*domain.Voyage, error) {
	if req.ID == "" || req.DepartureAt.IsZero() {
		return nil, fmt.Errorf("%w: voyage id and departure are required", domain.ErrInvalidState)
	}
	var v *domain.Voyage
	err := s.store.Update(func(d *store.Data) error {
		if _, ok := d.Voyages[req.ID]; ok {
			return fmt.Errorf("%w: voyage %s exists", domain.ErrConflict, req.ID)
		}
		nv := domain.NewVoyage(req.ID, req.Route, req.OriginPort, req.DestPort, req.Berth,
			req.DepartureAt, req.ArrivalAt, req.RefrigeratedSlots, req.DrySlots)
		d.Voyages[req.ID] = nv
		v = nv
		return nil
	})
	return v, err
}

// Get returns a voyage by id.
func (s *Service) Get(voyageID string) (*domain.Voyage, bool) {
	var v *domain.Voyage
	s.store.View(func(d *store.Data) { v = d.Voyages[voyageID] })
	return v, v != nil
}

// List returns all voyages.
func (s *Service) List() []*domain.Voyage {
	var out []*domain.Voyage
	s.store.View(func(d *store.Data) {
		for _, v := range d.Voyages {
			out = append(out, v)
		}
	})
	return out
}

// MarkDelayed records a voyage delay and auto-triggers re-appointment of all
// affected trucks, re-allocated by original priority.
func (s *Service) MarkDelayed(voyageID string) (*domain.Voyage, error) {
	var v *domain.Voyage
	err := s.store.Update(func(d *store.Data) error {
		voy := d.Voyages[voyageID]
		if voy == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
		}
		if voy.Status == domain.VoyageDeparted || voy.Status == domain.VoyageArrived {
			return fmt.Errorf("%w: voyage %s already %s", domain.ErrInvalidState, voyageID, voy.Status)
		}
		voy.Status = domain.VoyageDelayed
		v = voy
		return nil
	})
	if err != nil {
		return v, err
	}
	s.portapp.RescheduleForVoyage(voyageID)
	return v, nil
}

// MarkDeparted records that a voyage has departed.
func (s *Service) MarkDeparted(voyageID string) (*domain.Voyage, error) {
	return s.transition(voyageID, domain.VoyageDeparted)
}

// MarkArrived records arrival, enabling 到港签收.
func (s *Service) MarkArrived(voyageID string) (*domain.Voyage, error) {
	return s.transition(voyageID, domain.VoyageArrived)
}

func (s *Service) transition(voyageID string, to domain.VoyageStatus) (*domain.Voyage, error) {
	var v *domain.Voyage
	err := s.store.Update(func(d *store.Data) error {
		voy := d.Voyages[voyageID]
		if voy == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
		}
		if voy.Status == to {
			v = voy
			return nil
		}
		voy.Status = to
		v = voy
		return nil
	})
	return v, err
}

// ChangeBerth changes the berth plan and re-sequences appointments so that
// peak-season new-energy orders are prioritised and regular cargo postponed.
func (s *Service) ChangeBerth(voyageID, newBerth string) (*domain.Voyage, error) {
	var v *domain.Voyage
	err := s.store.Update(func(d *store.Data) error {
		voy := d.Voyages[voyageID]
		if voy == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, voyageID)
		}
		voy.Berth = newBerth
		v = voy
		return nil
	})
	if err != nil {
		return v, err
	}
	s.portapp.ResequenceByPriority(voyageID)
	return v, nil
}

// RequestPortChange starts a dual-signature port change (改港). The change is
// not applied until both the dispatch specialist and the destination-port agent
// have signed. Validates the status, the 72h window and the two-change cap.
func (s *Service) RequestPortChange(bookingID, newPort string) (*domain.Booking, error) {
	var bk *domain.Booking
	err := s.store.Update(func(d *store.Data) error {
		b := d.Bookings[bookingID]
		if b == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
		}
		voy := d.Voyages[b.VoyageID]
		if voy == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, b.VoyageID)
		}
		if err := b.ValidatePortChange(voy, s.clock.Now()); err != nil {
			return err
		}
		b.RequestPortChange(newPort, s.clock.Now())
		bk = b
		return nil
	})
	return bk, err
}

// SignPortChange records a signature. When both roles have signed the change is
// approved and applied to the voyage's destination.
func (s *Service) SignPortChange(bookingID string, role domain.SignatureRole) (*domain.Booking, error) {
	var bk *domain.Booking
	err := s.store.Update(func(d *store.Data) error {
		b := d.Bookings[bookingID]
		if b == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
		}
		if b.PendingPortChange == nil {
			return fmt.Errorf("%w: no pending port change for booking %s", domain.ErrNotFound, bookingID)
		}
		if err := b.SignPortChange(role, s.clock.Now()); err != nil {
			return err
		}
		if b.PendingPortChange != nil && b.PendingPortChange.Approved {
			if voy := d.Voyages[b.VoyageID]; voy != nil {
				b.ApplyApprovedPortChange(voy)
			}
		}
		bk = b
		return nil
	})
	return bk, err
}
