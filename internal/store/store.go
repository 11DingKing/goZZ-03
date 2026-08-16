package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"arcticdispatch/internal/domain"
)

// Data is the on-disk serialisable snapshot of all aggregates.
type Data struct {
	Voyages      map[string]*domain.Voyage          `json:"voyages"`
	Bookings     map[string]*domain.Booking         `json:"bookings"`
	Cargo        map[string]*domain.Cargo           `json:"cargo"`
	Appointments map[string]*domain.PortAppointment `json:"appointments"`
	Manifests    map[string]*domain.Manifest        `json:"manifests"`
	Arrivals     map[string]*domain.Arrival         `json:"arrivals"`
	Waitlist     []*WaitlistEntry                   `json:"waitlist"`
	Idempotency  map[string]*IdempotentResult       `json:"idempotency"`
	Counters     map[string]int                     `json:"counters"`
}

// WaitlistEntry is a queued booking awaiting slot promotion (候补).
type WaitlistEntry struct {
	BookingID  string          `json:"booking_id"`
	VoyageID   string          `json:"voyage_id"`
	SlotType   domain.SlotType `json:"slot_type"`
	Quantity   int             `json:"quantity"`
	Priority   int             `json:"priority"`
	EnqueuedAt time.Time       `json:"enqueued_at"`
}

// IdempotentResult caches the outcome of an idempotent request so a retry
// returns the same aggregate without re-executing side effects. It is fully
// serialised so idempotency survives a restart.
type IdempotentResult struct {
	Key      string    `json:"key"`
	Kind     string    `json:"kind"`
	RefID    string    `json:"ref_id"`
	Status   int       `json:"status"`
	StoredAt time.Time `json:"stored_at"`
}

// Store is the persistence gateway. It keeps all aggregates in memory under a
// single RWMutex and atomically snapshots to a JSON file when configured. When
// path == "" it behaves as a pure in-memory store (used by tests).
type Store struct {
	mu   sync.RWMutex
	path string
	data *Data
}

// New opens or creates a store backed by path. A corrupt snapshot is treated as
// empty so the service can boot after a crash rather than failing permanently.
func New(path string) (*Store, error) {
	s := &Store{path: path, data: newData()}
	if path == "" {
		return s, nil
	}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

// NewInMemory returns a store with no disk backing.
func NewInMemory() *Store {
	s, _ := New("")
	return s
}

func newData() *Data {
	return &Data{
		Voyages:      make(map[string]*domain.Voyage),
		Bookings:     make(map[string]*domain.Booking),
		Cargo:        make(map[string]*domain.Cargo),
		Appointments: make(map[string]*domain.PortAppointment),
		Manifests:    make(map[string]*domain.Manifest),
		Arrivals:     make(map[string]*domain.Arrival),
		Waitlist:     []*WaitlistEntry{},
		Idempotency:  make(map[string]*IdempotentResult),
		Counters:     make(map[string]int),
	}
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		// corrupt snapshot: start fresh rather than failing to boot.
		return nil
	}
	if d.Voyages == nil {
		d = *newData()
	}
	s.data = &d
	return nil
}

// persist writes a snapshot atomically. The caller must hold at least a read
// lock (Update holds the write lock).
func (s *Store) persist() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Update runs fn under the exclusive write lock and persists the result. If fn
// returns an error no snapshot is written, leaving prior state intact.
func (s *Store) Update(fn func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.data); err != nil {
		return err
	}
	return s.persist()
}

// View runs fn under the shared read lock.
func (s *Store) View(fn func(*Data)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.data)
}

// Snapshot forces a synchronous flush of the in-memory state to disk.
func (s *Store) Snapshot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist()
}

// NextID returns the next deterministic identifier for a prefix. It must be
// called while the caller already holds the lock (i.e. inside Update).
func NextID(d *Data, prefix string) string {
	n := d.Counters[prefix]
	d.Counters[prefix] = n + 1
	return fmt.Sprintf("%s-%d", prefix, n+1)
}
