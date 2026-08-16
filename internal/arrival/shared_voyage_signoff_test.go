package arrival_test

import (
	"testing"
	"time"

	"arcticdispatch/internal/arrival"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

// TestSignOffIsPerBookingOnSharedVoyage signs off two different bookings that
// travel on the same arrived voyage. Each booking must get its own arrival
// record and reach the arrived state, while a repeated sign-off for the same
// booking stays idempotent.
func TestSignOffIsPerBookingOnSharedVoyage(t *testing.T) {
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := arrival.New(st, clk)
	st.Update(func(d *store.Data) error {
		v := domain.NewVoyage("V1", "r", "SHA", "ROT", "B1",
			clk.Now().Add(10*24*time.Hour), clk.Now().Add(20*24*time.Hour), 2, 0)
		v.Status = domain.VoyageArrived
		d.Voyages["V1"] = v
		d.Bookings["BK-1"] = &domain.Booking{ID: "BK-1", VoyageID: "V1", Status: domain.BookingManifested, SubmitTime: clk.Now()}
		d.Bookings["BK-2"] = &domain.Booking{ID: "BK-2", VoyageID: "V1", Status: domain.BookingManifested, SubmitTime: clk.Now()}
		return nil
	})

	ar1, err := svc.SignOff("BK-1", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if ar1.BookingID != "BK-1" {
		t.Fatalf("first sign-off returned arrival for %s, want BK-1", ar1.BookingID)
	}

	ar2, err := svc.SignOff("BK-2", "agent-2")
	if err != nil {
		t.Fatal(err)
	}
	if ar2.BookingID != "BK-2" {
		t.Fatalf("second sign-off returned arrival for %s, want BK-2", ar2.BookingID)
	}
	if ar2.ID == ar1.ID {
		t.Fatalf("each booking needs its own arrival record, both are %s", ar1.ID)
	}
	if ar2.SignedBy != "agent-2" {
		t.Fatalf("second arrival signed by %q, want agent-2", ar2.SignedBy)
	}
	for _, id := range []string{"BK-1", "BK-2"} {
		if got := getBooking(st, id).Status; got != domain.BookingArrived {
			t.Errorf("booking %s status = %s, want arrived", id, got)
		}
	}

	// Signing the same booking again returns its original record unchanged.
	again, err := svc.SignOff("BK-1", "agent-3")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != ar1.ID || again.SignedBy != "agent-1" {
		t.Fatalf("repeat sign-off should be idempotent, got id=%s signedBy=%s", again.ID, again.SignedBy)
	}
}
