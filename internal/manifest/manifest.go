package manifest

import (
	"fmt"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

// Service implements 舱单申报 (manifest declaration).
type Service struct {
	store *store.Store
	clock clock.Clock
}

// New creates a manifest service.
func New(s *store.Store, c clock.Clock) *Service {
	return &Service{store: s, clock: c}
}

// SubmitReq carries manifest data for a booking.
type SubmitReq struct {
	BookingID  string
	CargoDesc  string
	WeightTons float64
}

// Submit creates a submitted manifest for a confirmed booking. If a non-rejected
// manifest already exists for the booking it is returned unchanged (idempotent).
func (s *Service) Submit(req SubmitReq) (*domain.Manifest, error) {
	if req.WeightTons <= 0 {
		return nil, fmt.Errorf("%w: weight must be positive", domain.ErrInvalidState)
	}
	var m *domain.Manifest
	err := s.store.Update(func(d *store.Data) error {
		bk := d.Bookings[req.BookingID]
		if bk == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, req.BookingID)
		}
		if bk.Status != domain.BookingConfirmed {
			return fmt.Errorf("%w: booking %s status %s cannot submit manifest", domain.ErrInvalidState, req.BookingID, bk.Status)
		}
		for _, ex := range d.Manifests {
			if ex.BookingID == req.BookingID && ex.Status != domain.ManifestRejected {
				m = ex
				return nil
			}
		}
		id := store.NextID(d, "MF")
		now := s.clock.Now()
		nm := &domain.Manifest{
			ID: id, BookingID: req.BookingID, VoyageID: bk.VoyageID,
			CargoDesc: req.CargoDesc, WeightTons: req.WeightTons,
			Status: domain.ManifestSubmitted, SubmittedAt: now,
		}
		d.Manifests[id] = nm
		m = nm
		return nil
	})
	return m, err
}

// Declare moves a submitted manifest to declared and the booking to manifested,
// unlocking the arrival sign-off step.
func (s *Service) Declare(manifestID string) (*domain.Manifest, error) {
	var m *domain.Manifest
	err := s.store.Update(func(d *store.Data) error {
		mf := d.Manifests[manifestID]
		if mf == nil {
			return fmt.Errorf("%w: manifest %s", domain.ErrNotFound, manifestID)
		}
		if mf.Status == domain.ManifestDeclared {
			m = mf
			return nil
		}
		if mf.Status != domain.ManifestSubmitted {
			return fmt.Errorf("%w: manifest %s status %s cannot declare", domain.ErrInvalidState, manifestID, mf.Status)
		}
		bk := d.Bookings[mf.BookingID]
		if bk == nil {
			return fmt.Errorf("%w: booking %s", domain.ErrNotFound, mf.BookingID)
		}
		now := s.clock.Now()
		mf.Status = domain.ManifestDeclared
		mf.DeclaredAt = now
		bk.Status = domain.BookingManifested
		m = mf
		return nil
	})
	return m, err
}

// Reject rejects a submitted manifest so it can be re-submitted after correction.
func (s *Service) Reject(manifestID, reason string) (*domain.Manifest, error) {
	var m *domain.Manifest
	err := s.store.Update(func(d *store.Data) error {
		mf := d.Manifests[manifestID]
		if mf == nil {
			return fmt.Errorf("%w: manifest %s", domain.ErrNotFound, manifestID)
		}
		if mf.Status != domain.ManifestSubmitted {
			return fmt.Errorf("%w: manifest %s status %s cannot reject", domain.ErrInvalidState, manifestID, mf.Status)
		}
		mf.Status = domain.ManifestRejected
		mf.Rejection = reason
		mf.RejectedAt = s.clock.Now()
		m = mf
		return nil
	})
	return m, err
}
