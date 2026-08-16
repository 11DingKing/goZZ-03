package arrival_test

import (
	"errors"
	"testing"
	"time"

	"arcticdispatch/internal/arrival"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

func getBooking(st *store.Store, id string) *domain.Booking {
	var b *domain.Booking
	st.View(func(d *store.Data) { b = d.Bookings[id] })
	return b
}

func TestArrivalSignOffRequiresArrivedVoyage(t *testing.T) {
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := arrival.New(st, clk)
	st.Update(func(d *store.Data) error {
		d.Voyages["V1"] = domain.NewVoyage("V1", "r", "SHA", "ROT", "B1",
			clk.Now().Add(10*24*time.Hour), clk.Now().Add(20*24*time.Hour), 1, 0)
		d.Bookings["BK-1"] = &domain.Booking{ID: "BK-1", VoyageID: "V1", Status: domain.BookingManifested, SubmitTime: clk.Now()}
		return nil
	})

	// Voyage not yet arrived -> sign-off rejected.
	if _, err := svc.SignOff("BK-1", "agent"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("want ErrInvalidState before arrival, got %v", err)
	}

	// Mark the voyage arrived, then sign-off succeeds.
	st.Update(func(d *store.Data) error { d.Voyages["V1"].Status = domain.VoyageArrived; return nil })
	ar, err := svc.SignOff("BK-1", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if ar.Status != domain.ArrivalSigned {
		t.Fatalf("want signed, got %s", ar.Status)
	}
	if getBooking(st, "BK-1").Status != domain.BookingArrived {
		t.Fatalf("booking should be arrived, got %s", getBooking(st, "BK-1").Status)
	}
	// Idempotent: signing off again is a no-op.
	if _, err := svc.SignOff("BK-1", "agent2"); err != nil {
		t.Fatalf("idempotent sign-off failed: %v", err)
	}

	// A non-manifested booking cannot be signed off.
	st.Update(func(d *store.Data) error {
		d.Bookings["BK-2"] = &domain.Booking{ID: "BK-2", VoyageID: "V1", Status: domain.BookingConfirmed, SubmitTime: clk.Now()}
		return nil
	})
	if _, err := svc.SignOff("BK-2", "agent"); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("non-manifested: want ErrInvalidState, got %v", err)
	}
}
