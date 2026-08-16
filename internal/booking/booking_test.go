package booking_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

func newBookingEnv(t *testing.T, paymentTimeout time.Duration) (*store.Store, *booking.Service, *clock.Fake) {
	t.Helper()
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return st, booking.New(st, clk, paymentTimeout), clk
}

func seed(t *testing.T, st *store.Store, fn func(*store.Data)) {
	t.Helper()
	if err := st.Update(func(d *store.Data) error { fn(d); return nil }); err != nil {
		t.Fatal(err)
	}
}

func voyageAt(clk *clock.Fake, id string, refrig, dry int) *domain.Voyage {
	return domain.NewVoyage(id, "r", "SHA", "ROT", "B1",
		clk.Now().Add(10*24*time.Hour), clk.Now().Add(20*24*time.Hour), refrig, dry)
}

func TestCreateBookingFreezesAndPays(t *testing.T) {
	st, svc, clk := newBookingEnv(t, domain.PaymentTimeout)
	seed(t, st, func(d *store.Data) {
		d.Cargo["C1"] = &domain.Cargo{ID: "C1", Type: domain.CargoEnergyStorage, TempConfirmed: true}
		d.Voyages["V1"] = voyageAt(clk, "V1", 1, 0)
	})
	bk, err := svc.CreateBooking(booking.CreateBookingReq{
		SalesManagerID: "S1", CargoID: "C1", VoyageID: "V1",
		SlotType: domain.SlotRefrigerated, Quantity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bk.Status != domain.BookingFrozen {
		t.Fatalf("want frozen, got %s", bk.Status)
	}
	if bk.SlotIndex < 0 {
		t.Fatal("no slot assigned")
	}
	paid, err := svc.Pay(bk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != domain.BookingConfirmed {
		t.Fatalf("want confirmed, got %s", paid.Status)
	}
	// Paying again is idempotent.
	if _, err := svc.Pay(bk.ID); err != nil {
		t.Fatalf("idempotent pay failed: %v", err)
	}
}

func TestRefrigeratedRejectsUnconfirmedOrRegularCargo(t *testing.T) {
	st, svc, clk := newBookingEnv(t, domain.PaymentTimeout)
	seed(t, st, func(d *store.Data) {
		d.Cargo["CU"] = &domain.Cargo{ID: "CU", Type: domain.CargoEnergyStorage} // not confirmed
		d.Cargo["CR"] = &domain.Cargo{ID: "CR", Type: domain.CargoRegular, TempConfirmed: true}
		d.Voyages["V1"] = voyageAt(clk, "V1", 1, 1)
	})
	if _, err := svc.CreateBooking(booking.CreateBookingReq{CargoID: "CU", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1}); !errors.Is(err, domain.ErrTempControlFailed) {
		t.Fatalf("unconfirmed on refrigerated: want ErrTempControlFailed, got %v", err)
	}
	if _, err := svc.CreateBooking(booking.CreateBookingReq{CargoID: "CR", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1}); !errors.Is(err, domain.ErrTempControlFailed) {
		t.Fatalf("regular on refrigerated: want ErrTempControlFailed, got %v", err)
	}
	// Regular cargo is allowed on a dry slot.
	bk, err := svc.CreateBooking(booking.CreateBookingReq{CargoID: "CR", VoyageID: "V1", SlotType: domain.SlotDry, Quantity: 1})
	if err != nil {
		t.Fatalf("regular on dry: %v", err)
	}
	if bk.Status != domain.BookingFrozen {
		t.Fatalf("regular on dry should freeze, got %s", bk.Status)
	}
}

func TestPaymentTimeoutReleasesAndPromotesWaitlist(t *testing.T) {
	st, svc, clk := newBookingEnv(t, 50*time.Millisecond)
	seed(t, st, func(d *store.Data) {
		d.Cargo["C1"] = &domain.Cargo{ID: "C1", Type: domain.CargoEnergyStorage, TempConfirmed: true}
		d.Cargo["C2"] = &domain.Cargo{ID: "C2", Type: domain.CargoPowerBattery, TempConfirmed: true}
		d.Voyages["V1"] = voyageAt(clk, "V1", 1, 0) // a single refrigerated slot
	})
	a, err := svc.CreateBooking(booking.CreateBookingReq{CargoID: "C1", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateBooking(booking.CreateBookingReq{CargoID: "C2", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != domain.BookingWaitlisted {
		t.Fatalf("second booking should be waitlisted, got %s", b.Status)
	}

	// Advance past the 50ms payment window.
	clk.Advance(60 * time.Millisecond)
	released := svc.ReconcileExpired()
	if len(released) != 1 || released[0] != a.ID {
		t.Fatalf("want [%s] released, got %v", a.ID, released)
	}
	ab, _ := svc.Get(a.ID)
	if ab.Status != domain.BookingReleased {
		t.Fatalf("a should be released, got %s", ab.Status)
	}
	bb, _ := svc.Get(b.ID)
	if bb.Status != domain.BookingFrozen {
		t.Fatalf("b should be promoted to frozen, got %s", bb.Status)
	}
	// The promoted booking gets a fresh freeze window and can pay.
	if _, err := svc.Pay(b.ID); err != nil {
		t.Fatalf("promoted booking pay failed: %v", err)
	}
}

func TestConcurrentBookingLastSlotFirstSubmitterWins(t *testing.T) {
	st, svc, clk := newBookingEnv(t, domain.PaymentTimeout)
	seed(t, st, func(d *store.Data) {
		d.Cargo["CA"] = &domain.Cargo{ID: "CA", Type: domain.CargoEnergyStorage, TempConfirmed: true}
		d.Cargo["CB"] = &domain.Cargo{ID: "CB", Type: domain.CargoPVModule, TempConfirmed: true}
		d.Voyages["V1"] = voyageAt(clk, "V1", 1, 0) // exactly one refrigerated slot
	})
	var bka, bkb *domain.Booking
	var erra, errb error
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		bka, erra = svc.CreateBooking(booking.CreateBookingReq{CargoID: "CA", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1})
	}()
	go func() {
		defer wg.Done()
		<-start
		bkb, errb = svc.CreateBooking(booking.CreateBookingReq{CargoID: "CB", VoyageID: "V1", SlotType: domain.SlotRefrigerated, Quantity: 1})
	}()
	close(start)
	wg.Wait()
	if erra != nil || errb != nil {
		t.Fatalf("unexpected errors: %v %v", erra, errb)
	}
	frozen, waitlisted := 0, 0
	for _, b := range []*domain.Booking{bka, bkb} {
		switch b.Status {
		case domain.BookingFrozen:
			frozen++
		case domain.BookingWaitlisted:
			waitlisted++
		}
	}
	if frozen != 1 || waitlisted != 1 {
		t.Fatalf("want exactly 1 frozen + 1 waitlisted, got %d frozen + %d waitlisted", frozen, waitlisted)
	}
}

func TestIdempotentCreateBookingRetries(t *testing.T) {
	st, svc, clk := newBookingEnv(t, domain.PaymentTimeout)
	seed(t, st, func(d *store.Data) {
		d.Cargo["C1"] = &domain.Cargo{ID: "C1", Type: domain.CargoEnergyStorage, TempConfirmed: true}
		d.Voyages["V1"] = voyageAt(clk, "V1", 2, 0)
	})
	req := booking.CreateBookingReq{
		IdempotencyKey: "K1", SalesManagerID: "S1", CargoID: "C1", VoyageID: "V1",
		SlotType: domain.SlotRefrigerated, Quantity: 1,
	}
	bk1, err := svc.CreateBooking(req)
	if err != nil {
		t.Fatal(err)
	}
	bk2, err := svc.CreateBooking(req) // same idempotency key
	if err != nil {
		t.Fatal(err)
	}
	if bk1.ID != bk2.ID {
		t.Fatalf("idempotency returned different bookings: %s vs %s", bk1.ID, bk2.ID)
	}
	var count int
	st.View(func(d *store.Data) { count = len(d.Bookings) })
	if count != 1 {
		t.Fatalf("want 1 booking created, got %d", count)
	}
}
