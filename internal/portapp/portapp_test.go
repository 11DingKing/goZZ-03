package portapp_test

import (
	"errors"
	"testing"
	"time"

	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/store"
)

func newPortappEnv(t *testing.T) (*store.Store, *clock.Fake, *portapp.Service) {
	t.Helper()
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return st, clk, portapp.New(st, clk)
}

func TestAppointmentRequiresConfirmedBooking(t *testing.T) {
	st, clk, svc := newPortappEnv(t)
	ws, we := clk.Now(), clk.Now().Add(time.Hour)

	// Unknown booking.
	if _, err := svc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: "BK-x", TruckID: "T", WindowStart: ws, WindowEnd: we}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown booking: want ErrNotFound, got %v", err)
	}
	// Pending booking cannot be appointed.
	st.Update(func(d *store.Data) error {
		d.Bookings["BK-1"] = &domain.Booking{ID: "BK-1", VoyageID: "V1", Status: domain.BookingPending}
		return nil
	})
	if _, err := svc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: "BK-1", TruckID: "T", WindowStart: ws, WindowEnd: we}); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("pending booking: want ErrInvalidState, got %v", err)
	}
	// Bad window.
	st.Update(func(d *store.Data) error { d.Bookings["BK-1"].Status = domain.BookingConfirmed; return nil })
	if _, err := svc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: "BK-1", TruckID: "T", WindowStart: we, WindowEnd: ws}); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("bad window: want ErrInvalidState, got %v", err)
	}
	// Confirmed booking succeeds.
	apt, err := svc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: "BK-1", TruckID: "T", WindowStart: ws, WindowEnd: we})
	if err != nil {
		t.Fatal(err)
	}
	if apt.Status != domain.AptScheduled {
		t.Fatalf("want scheduled, got %s", apt.Status)
	}
	// Repeat call is idempotent (returns the same active appointment).
	apt2, err := svc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: "BK-1", TruckID: "T", WindowStart: ws, WindowEnd: we})
	if err != nil || apt2.ID != apt.ID {
		t.Fatalf("idempotent appoint: err=%v same=%v", err, apt2.ID == apt.ID)
	}
	// Complete.
	if _, err := svc.CompleteAppointment(apt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteAppointment(apt.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("double complete: want ErrInvalidState, got %v", err)
	}
}
