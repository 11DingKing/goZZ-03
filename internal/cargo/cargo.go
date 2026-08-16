package cargo

import (
	"fmt"
	"time"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

// Service manages cargo registration and temperature-control confirmation.
type Service struct {
	store *store.Store
	clock clock.Clock
}

// New creates a cargo service.
func New(s *store.Store, c clock.Clock) *Service {
	return &Service{store: s, clock: c}
}

// CreateReq carries the data to register a cargo.
type CreateReq struct {
	ID         string
	Type       domain.CargoType
	TempClass  domain.TemperatureClass
	OwnerID    string
	PeakSeason bool
}

// Create registers a cargo. Temperature is not yet confirmed; a warehouse
// supervisor must call ConfirmTemp before a refrigerated slot can be allocated.
func (s *Service) Create(req CreateReq) (*domain.Cargo, error) {
	if req.ID == "" || req.Type == "" {
		return nil, fmt.Errorf("%w: id and type required", domain.ErrInvalidState)
	}
	var c *domain.Cargo
	err := s.store.Update(func(d *store.Data) error {
		if _, ok := d.Cargo[req.ID]; ok {
			return fmt.Errorf("%w: cargo %s exists", domain.ErrConflict, req.ID)
		}
		nc := &domain.Cargo{
			ID: req.ID, Type: req.Type, TempClass: req.TempClass,
			OwnerID: req.OwnerID, PeakSeason: req.PeakSeason,
		}
		d.Cargo[req.ID] = nc
		c = nc
		return nil
	})
	return c, err
}

// ConfirmTemp is called by the warehouse supervisor (仓储主管) to confirm the
// temperature-control attribute, the gate that enables refrigerated slot
// allocation for new-energy cargo.
func (s *Service) ConfirmTemp(id string) (*domain.Cargo, error) {
	var c *domain.Cargo
	err := s.store.Update(func(d *store.Data) error {
		cc := d.Cargo[id]
		if cc == nil {
			return fmt.Errorf("%w: cargo %s", domain.ErrNotFound, id)
		}
		cc.TempConfirmed = true
		cc.ConfirmedAt = s.clock.Now()
		c = cc
		return nil
	})
	return c, err
}

// Get returns a cargo by id.
func (s *Service) Get(id string) (*domain.Cargo, bool) {
	var c *domain.Cargo
	s.store.View(func(d *store.Data) { c = d.Cargo[id] })
	return c, c != nil
}

// Now is exposed for services that compose cargo state (kept minimal).
func (s *Service) Now() time.Time { return s.clock.Now() }
