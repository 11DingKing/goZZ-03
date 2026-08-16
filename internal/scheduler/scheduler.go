package scheduler

import (
	"context"
	"sync"
	"time"

	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/portapp"
)

// Scheduler runs the background reconciliation that provides failure recovery:
// it releases frozen slots whose payment window elapsed and promotes the
// waitlist, and re-schedules appointments for voyages left delayed by a crash.
type Scheduler struct {
	booking *booking.Service
	portapp *portapp.Service
	tick    time.Duration
	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a scheduler that reconciles every tick.
func New(b *booking.Service, pa *portapp.Service, tick time.Duration) *Scheduler {
	return &Scheduler{booking: b, portapp: pa, tick: tick}
}

// Start launches the background loop. It returns immediately.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(s.tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.tickOnce()
			}
		}
	}()
}

// tickOnce performs one reconciliation pass: release expired freezes and
// promote the waitlist, then re-schedule any delayed-voyage appointments.
func (s *Scheduler) tickOnce() {
	s.booking.ReconcileExpired()
	s.portapp.Reconcile()
}

// ReconcileNow runs one pass synchronously. Used by tests and on startup.
func (s *Scheduler) ReconcileNow() { s.tickOnce() }

// Stop signals the loop to exit and waits for it to drain.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()
	s.wg.Wait()
}
