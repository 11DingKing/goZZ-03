package manifest_test

import (
	"errors"
	"testing"
	"time"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/manifest"
	"arcticdispatch/internal/store"
)

func newManifestEnv(t *testing.T) (*store.Store, *clock.Fake, *manifest.Service) {
	t.Helper()
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	st.Update(func(d *store.Data) error {
		d.Voyages["V1"] = domain.NewVoyage("V1", "r", "SHA", "ROT", "B1",
			clk.Now().Add(10*24*time.Hour), clk.Now().Add(20*24*time.Hour), 1, 0)
		return nil
	})
	return st, clk, manifest.New(st, clk)
}

func getBooking(st *store.Store, id string) *domain.Booking {
	var b *domain.Booking
	st.View(func(d *store.Data) { b = d.Bookings[id] })
	return b
}

func TestManifestSubmitDeclareTransitionsBooking(t *testing.T) {
	st, clk, svc := newManifestEnv(t)
	st.Update(func(d *store.Data) error {
		d.Bookings["BK-1"] = &domain.Booking{ID: "BK-1", VoyageID: "V1", CargoID: "C1", Status: domain.BookingConfirmed, SubmitTime: clk.Now()}
		d.Bookings["BK-2"] = &domain.Booking{ID: "BK-2", VoyageID: "V1", CargoID: "C2", Status: domain.BookingPending, SubmitTime: clk.Now()}
		return nil
	})

	// Non-confirmed booking cannot submit a manifest.
	if _, err := svc.Submit(manifest.SubmitReq{BookingID: "BK-2", CargoDesc: "x", WeightTons: 1}); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("pending submit: want ErrInvalidState, got %v", err)
	}
	// Invalid weight rejected.
	if _, err := svc.Submit(manifest.SubmitReq{BookingID: "BK-1", CargoDesc: "x", WeightTons: 0}); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("zero weight: want ErrInvalidState, got %v", err)
	}

	m, err := svc.Submit(manifest.SubmitReq{BookingID: "BK-1", CargoDesc: "batteries", WeightTons: 12.5})
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.ManifestSubmitted {
		t.Fatalf("want submitted, got %s", m.Status)
	}
	// Idempotent: a second submit returns the same manifest.
	m2, err := svc.Submit(manifest.SubmitReq{BookingID: "BK-1", CargoDesc: "batteries", WeightTons: 12.5})
	if err != nil || m2.ID != m.ID {
		t.Fatalf("idempotent submit: err=%v same=%v", err, m2.ID == m.ID)
	}
	// Declare moves manifest -> declared and booking -> manifested.
	if _, err := svc.Declare(m.ID); err != nil {
		t.Fatal(err)
	}
	if getBooking(st, "BK-1").Status != domain.BookingManifested {
		t.Fatalf("booking should be manifested, got %s", getBooking(st, "BK-1").Status)
	}
	// Reject path: a rejected manifest allows a fresh submit.
	st.Update(func(d *store.Data) error {
		d.Bookings["BK-3"] = &domain.Booking{ID: "BK-3", VoyageID: "V1", Status: domain.BookingConfirmed, SubmitTime: clk.Now()}
		return nil
	})
	m3, _ := svc.Submit(manifest.SubmitReq{BookingID: "BK-3", CargoDesc: "x", WeightTons: 2})
	if _, err := svc.Reject(m3.ID, "missing docs"); err != nil {
		t.Fatal(err)
	}
	m4, _ := svc.Submit(manifest.SubmitReq{BookingID: "BK-3", CargoDesc: "x", WeightTons: 2})
	if m4.ID == m3.ID {
		t.Fatal("re-submit after reject should create a new manifest")
	}
}
