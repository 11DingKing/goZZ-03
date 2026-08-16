package scheduler_test

import (
	"context"
	"testing"
	"time"

	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/scheduler"
	"arcticdispatch/internal/store"
)

func setupExpiry(t *testing.T) (*store.Store, *clock.Fake, *booking.Service, *scheduler.Scheduler, *domain.Booking) {
	t.Helper()
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bookingSvc := booking.New(st, clk, 50*time.Millisecond)
	portappSvc := portapp.New(st, clk)
	sched := scheduler.New(bookingSvc, portappSvc, 5*time.Millisecond)
	st.Update(func(d *store.Data) error {
		d.Cargo["C1"] = &domain.Cargo{ID: "C1", Type: domain.CargoEnergyStorage, TempConfirmed: true}
		d.Voyages["V1"] = domain.NewVoyage("V1", "r", "SHA", "ROT", "B1",
			clk.Now().Add(10*24*time.Hour), clk.Now().Add(20*24*time.Hour), 1, 0)
		return nil
	})
	bk, err := bookingSvc.CreateBooking(booking.CreateBookingReq{CargoID: "C1", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	return st, clk, bookingSvc, sched, bk
}

func TestSchedulerReconcileNowReleasesExpired(t *testing.T) {
	_, clk, bookingSvc, sched, bk := setupExpiry(t)
	clk.Advance(60 * time.Millisecond)
	sched.ReconcileNow()
	b, _ := bookingSvc.Get(bk.ID)
	if b.Status != domain.BookingReleased {
		t.Fatalf("want released, got %s", b.Status)
	}
}

func TestSchedulerBackgroundLoopReleasesExpired(t *testing.T) {
	_, clk, bookingSvc, sched, bk := setupExpiry(t)
	clk.Advance(60 * time.Millisecond) // past the 50ms freeze window

	sched.Start(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := bookingSvc.Get(bk.ID); b != nil && b.Status == domain.BookingReleased {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	sched.Stop()

	b, _ := bookingSvc.Get(bk.ID)
	if b.Status != domain.BookingReleased {
		t.Fatalf("background loop did not release: %s", b.Status)
	}
}
