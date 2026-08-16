package booking

import (
	"fmt"
	"time"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

// Service implements 订舱, payment, cancellation, waitlist promotion and
// payment-timeout reconciliation.
type Service struct {
	store          *store.Store
	clock          clock.Clock
	paymentTimeout time.Duration
}

// New creates a booking service. paymentTimeout defaults to the domain rule
// (10 minutes) when non-positive, allowing tests to use a shorter window.
func New(s *store.Store, c clock.Clock, paymentTimeout time.Duration) *Service {
	if paymentTimeout <= 0 {
		paymentTimeout = domain.PaymentTimeout
	}
	return &Service{store: s, clock: c, paymentTimeout: paymentTimeout}
}

// PaymentTimeout exposes the configured freeze window.
func (s *Service) PaymentTimeout() time.Duration { return s.paymentTimeout }

// CreateBookingReq is the 订舱 request submitted by a sales manager.
type CreateBookingReq struct {
	IdempotencyKey string
	SalesManagerID string
	CargoID        string
	VoyageID       string
	SlotType       domain.SlotType
	Quantity       int
}

// CreateBooking submits a booking request. It validates temperature control,
// atomically freezes slots for the first submitter, or enqueues the request on
// the waitlist when not enough free slots remain. The call is idempotent by key:
// a repeated key returns the original booking without creating a duplicate.
func (s *Service) CreateBooking(req CreateBookingReq) (*domain.Booking, error) {
	if req.Quantity <= 0 {
		return nil, fmt.Errorf("%w: quantity must be positive", domain.ErrInvalidState)
	}
	var result *domain.Booking
	err := s.store.Update(func(d *store.Data) error {
		if req.IdempotencyKey != "" {
			if r, ok := d.Idempotency[req.IdempotencyKey]; ok && r.Kind == "create_booking" {
				if b := d.Bookings[r.RefID]; b != nil {
					result = b
					return nil
				}
			}
		}
		cargo := d.Cargo[req.CargoID]
		if cargo == nil {
			return fmt.Errorf("%w: cargo %s", domain.ErrNotFound, req.CargoID)
		}
		if !cargo.EligibleForSlot(req.SlotType) {
			return fmt.Errorf("%w: cargo %s not eligible for %s slot", domain.ErrTempControlFailed, req.CargoID, req.SlotType)
		}
		voyage := d.Voyages[req.VoyageID]
		if voyage == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, req.VoyageID)
		}
		now := s.clock.Now()
		free := voyage.FreeSlots(req.SlotType)
		if len(free) < req.Quantity {
			// Not enough slots: enqueue as waitlist (候补). The first submitter
			// for the last batch already holds the lock and wins; this caller
			// loses and is queued.
			id := store.NextID(d, "BK")
			bk := domain.NewBooking(id, req.IdempotencyKey, req.SalesManagerID, req.CargoID, req.VoyageID, req.SlotType, req.Quantity, now, cargo)
			bk.Status = domain.BookingWaitlisted
			d.Bookings[id] = bk
			d.Waitlist = append(d.Waitlist, &store.WaitlistEntry{
				BookingID: id, VoyageID: req.VoyageID, SlotType: req.SlotType,
				Quantity: req.Quantity, Priority: bk.Priority, EnqueuedAt: now,
			})
			s.recordIdempotency(d, req.IdempotencyKey, "create_booking", id, 202, now)
			result = bk
			return nil
		}
		id := store.NextID(d, "BK")
		bk := domain.NewBooking(id, req.IdempotencyKey, req.SalesManagerID, req.CargoID, req.VoyageID, req.SlotType, req.Quantity, now, cargo)
		for i := 0; i < req.Quantity; i++ {
			if err := voyage.FreezeSlot(free[i], id, now); err != nil {
				return err
			}
		}
		bk.SlotIndex = free[0]
		bk.Status = domain.BookingFrozen
		bk.FrozenAt = now
		d.Bookings[id] = bk
		s.recordIdempotency(d, req.IdempotencyKey, "create_booking", id, 201, now)
		result = bk
		return nil
	})
	return result, err
}

func (s *Service) recordIdempotency(d *store.Data, key, kind, refID string, status int, now time.Time) {
	if key == "" {
		return
	}
	d.Idempotency[key] = &store.IdempotentResult{
		Key: key, Kind: kind, RefID: refID, Status: status, StoredAt: now,
	}
}

// Pay records payment for a frozen booking within the freeze window, allocating
// the slot and moving the booking to Confirmed. Returns ErrPaymentTimeout (and
// releases the slot + promotes the waitlist) if the window has elapsed. Paying
// an already-confirmed booking is a no-op.
func (s *Service) Pay(bookingID string) (*domain.Booking, error) {
	var result *domain.Booking
	err := s.store.Update(func(d *store.Data) error {
		bk := d.Bookings[bookingID]
		if bk == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
		}
		if bk.Status == domain.BookingConfirmed {
			result = bk
			return nil
		}
		if bk.Status != domain.BookingFrozen {
			return fmt.Errorf("%w: booking %s status %s cannot pay", domain.ErrInvalidState, bookingID, bk.Status)
		}
		now := s.clock.Now()
		if now.Sub(bk.FrozenAt) > s.paymentTimeout {
			s.releaseFrozen(d, bk)
			s.promoteFromWaitlistLocked(d, bk.VoyageID, bk.SlotType)
			return fmt.Errorf("%w: booking %s freeze expired", domain.ErrPaymentTimeout, bookingID)
		}
		voyage := d.Voyages[bk.VoyageID]
		if voyage == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, bk.VoyageID)
		}
		for i := range voyage.Slots {
			if voyage.Slots[i].BookingID == bookingID && voyage.Slots[i].Status == domain.SlotFrozen {
				if err := voyage.AllocateSlot(i, bookingID); err != nil {
					return err
				}
			}
		}
		bk.Status = domain.BookingConfirmed
		bk.PaidAt = now
		result = bk
		return nil
	})
	return result, err
}

// Cancel releases a booking's slots and promotes the next waitlisted customer.
func (s *Service) Cancel(bookingID string) (*domain.Booking, error) {
	var result *domain.Booking
	err := s.store.Update(func(d *store.Data) error {
		bk := d.Bookings[bookingID]
		if bk == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
		}
		if bk.Status == domain.BookingCancelled || bk.Status == domain.BookingReleased {
			result = bk
			return nil
		}
		switch bk.Status {
		case domain.BookingFrozen, domain.BookingConfirmed, domain.BookingManifested:
		default:
			return fmt.Errorf("%w: booking %s status %s cannot cancel", domain.ErrInvalidState, bookingID, bk.Status)
		}
		voyage := d.Voyages[bk.VoyageID]
		if voyage != nil {
			for i := range voyage.Slots {
				if voyage.Slots[i].BookingID == bookingID {
					voyage.ReleaseSlot(i)
				}
			}
		}
		bk.Status = domain.BookingCancelled
		s.promoteFromWaitlistLocked(d, bk.VoyageID, bk.SlotType)
		result = bk
		return nil
	})
	return result, err
}

// Get returns a booking by id.
func (s *Service) Get(bookingID string) (*domain.Booking, bool) {
	var bk *domain.Booking
	s.store.View(func(d *store.Data) { bk = d.Bookings[bookingID] })
	return bk, bk != nil
}

// PromoteWaitlist promotes the highest-priority, earliest-enqueued waitlisted
// booking for a voyage/slot-type if a slot is free. Returns the promoted booking.
func (s *Service) PromoteWaitlist(voyageID string, slotType domain.SlotType) (*domain.Booking, bool) {
	var promoted *domain.Booking
	s.store.Update(func(d *store.Data) error {
		promoted = s.promoteFromWaitlistLocked(d, voyageID, slotType)
		return nil
	})
	return promoted, promoted != nil
}

// ReconcileExpired releases every frozen booking whose payment window has
// elapsed and promotes the waitlist for each released batch. This is the
// failure-recovery path invoked by the scheduler (and on startup).
func (s *Service) ReconcileExpired() []string {
	var released []string
	s.store.Update(func(d *store.Data) error {
		now := s.clock.Now()
		type freed struct {
			voyageID string
			slotType domain.SlotType
		}
		var freedSlots []freed
		for id, bk := range d.Bookings {
			if bk.Status != domain.BookingFrozen {
				continue
			}
			if now.Sub(bk.FrozenAt) <= s.paymentTimeout {
				continue
			}
			s.releaseFrozen(d, bk)
			released = append(released, id)
			freedSlots = append(freedSlots, freed{bk.VoyageID, bk.SlotType})
		}
		for _, f := range freedSlots {
			s.promoteFromWaitlistLocked(d, f.voyageID, f.slotType)
		}
		return nil
	})
	return released
}

// releaseFrozen frees the frozen slots held by a booking and marks it released.
// Caller must hold the write lock.
func (s *Service) releaseFrozen(d *store.Data, bk *domain.Booking) {
	voyage := d.Voyages[bk.VoyageID]
	if voyage != nil {
		for i := range voyage.Slots {
			if voyage.Slots[i].BookingID == bk.ID && voyage.Slots[i].Status == domain.SlotFrozen {
				voyage.ReleaseSlot(i)
			}
		}
	}
	bk.Status = domain.BookingReleased
}

// promoteFromWaitlistLocked picks the best waitlisted booking for a voyage and
// slot type and freezes a freshly freed slot for it. Caller must hold the lock.
func (s *Service) promoteFromWaitlistLocked(d *store.Data, voyageID string, slotType domain.SlotType) *domain.Booking {
	best := -1
	for i, e := range d.Waitlist {
		if e.VoyageID != voyageID || e.SlotType != slotType {
			continue
		}
		cur := best == -1
		if !cur {
			cb := d.Waitlist[best]
			if e.Priority > cb.Priority || (e.Priority == cb.Priority && e.EnqueuedAt.Before(cb.EnqueuedAt)) {
				cur = true
			}
		}
		if cur {
			best = i
		}
	}
	if best == -1 {
		return nil
	}
	e := d.Waitlist[best]
	voyage := d.Voyages[voyageID]
	if voyage == nil {
		return nil
	}
	free := voyage.FreeSlots(slotType)
	if len(free) < e.Quantity {
		return nil
	}
	bk := d.Bookings[e.BookingID]
	if bk == nil {
		return nil
	}
	now := s.clock.Now()
	for i := 0; i < e.Quantity; i++ {
		if err := voyage.FreezeSlot(free[i], bk.ID, now); err != nil {
			return nil
		}
	}
	bk.SlotIndex = free[0]
	bk.Status = domain.BookingFrozen
	bk.FrozenAt = now
	d.Waitlist = append(d.Waitlist[:best], d.Waitlist[best+1:]...)
	return bk
}
