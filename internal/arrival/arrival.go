package arrival

import (
	"fmt"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

// Service implements 到港签收 (arrival sign-off), the final step of the chain.
type Service struct {
	store *store.Store
	clock clock.Clock
}

// New creates an arrival service.
func New(s *store.Store, c clock.Clock) *Service {
	return &Service{store: s, clock: c}
}

// SignOff records arrival sign-off for a manifested booking on an arrived
// voyage. Idempotent: signing off twice is a no-op.
func (s *Service) SignOff(bookingID, signedBy string) (*domain.Arrival, error) {
	var ar *domain.Arrival
	err := s.store.Update(func(d *store.Data) error {
		bk := d.Bookings[bookingID]
		if bk == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, bookingID)
		}
		// Idempotent: if this booking was already signed off, return the
		// existing arrival without re-validating state.
		for _, ex := range d.Arrivals {
			if ex.BookingID == bookingID {
				ar = ex
				return nil
			}
		}
		if bk.Status != domain.BookingManifested {
			return fmt.Errorf("%w: booking %s status %s cannot sign off", domain.ErrInvalidState, bookingID, bk.Status)
		}
		voy := d.Voyages[bk.VoyageID]
		if voy == nil {
			return fmt.Errorf("%w: voyage %s", domain.ErrNotFound, bk.VoyageID)
		}
		if voy.Status != domain.VoyageArrived {
			return fmt.Errorf("%w: voyage %s not arrived", domain.ErrInvalidState, bk.VoyageID)
		}
		id := store.NextID(d, "AR")
		now := s.clock.Now()
		na := &domain.Arrival{
			ID: id, BookingID: bookingID, VoyageID: bk.VoyageID,
			Status: domain.ArrivalSigned, SignedAt: now, SignedBy: signedBy,
		}
		d.Arrivals[id] = na
		bk.Status = domain.BookingArrived
		ar = na
		return nil
	})
	return ar, err
}
