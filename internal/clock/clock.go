package clock

import (
	"sync"
	"time"
)

// Clock abstracts the wall clock so business windows (payment timeout, port
// change deadline) and background reconciliation can be tested deterministically
// without sleeping.
type Clock interface {
	Now() time.Time
}

// System is the real wall-clock implementation used in production.
type System struct{}

func (System) Now() time.Time { return time.Now() }

// Fake is a controllable clock for tests. It is safe for concurrent use.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

// NewFake returns a fake clock anchored at t.
func NewFake(t time.Time) *Fake { return &Fake{t: t} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Set moves the fake clock to an absolute time.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}
