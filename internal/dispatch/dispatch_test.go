package dispatch_test

import (
	"errors"
	"testing"
	"time"

	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/dispatch"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/store"
)

func newDispatchEnv(t *testing.T) (*store.Store, *clock.Fake, *booking.Service, *portapp.Service, *dispatch.Service) {
	t.Helper()
	st := store.NewInMemory()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	bookingSvc := booking.New(st, clk, domain.PaymentTimeout)
	portappSvc := portapp.New(st, clk)
	dispatchSvc := dispatch.New(st, clk, portappSvc)
	return st, clk, bookingSvc, portappSvc, dispatchSvc
}

func mkVoyage(st *store.Store, clk *clock.Fake, id string, refrig, dry int) {
	st.Update(func(d *store.Data) error {
		d.Voyages[id] = domain.NewVoyage(id, "r", "SHA", "ROT", "B1",
			clk.Now().Add(10*24*time.Hour), clk.Now().Add(20*24*time.Hour), refrig, dry)
		return nil
	})
}

func seedCargo(st *store.Store, id string, ct domain.CargoType, peak bool) {
	st.Update(func(d *store.Data) error {
		d.Cargo[id] = &domain.Cargo{ID: id, Type: ct, TempConfirmed: true, PeakSeason: peak}
		return nil
	})
}

func confirmBooking(t *testing.T, bookingSvc *booking.Service, cargoID, voyageID string, slotType domain.SlotType) *domain.Booking {
	t.Helper()
	bk, err := bookingSvc.CreateBooking(booking.CreateBookingReq{SalesManagerID: "S1", CargoID: cargoID, VoyageID: voyageID, SlotType: slotType, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bookingSvc.Pay(bk.ID); err != nil {
		t.Fatal(err)
	}
	return bk
}

func TestPortChangeDualSignMaxAndWindow(t *testing.T) {
	st, clk, bookingSvc, _, dispatchSvc := newDispatchEnv(t)
	mkVoyage(st, clk, "V1", 1, 0)
	seedCargo(st, "C1", domain.CargoEnergyStorage, false)
	bk := confirmBooking(t, bookingSvc, "C1", "V1", domain.SlotRefrigerated)

	// Request a port change; it is pending until both roles sign.
	if rb, err := dispatchSvc.RequestPortChange(bk.ID, "HAM"); err != nil || rb.PendingPortChange == nil || rb.PendingPortChange.Approved {
		t.Fatalf("request: err=%v pending=%v approved=%v", err, rb.PendingPortChange, rb.PendingPortChange)
	}
	if sb, err := dispatchSvc.SignPortChange(bk.ID, domain.SigDispatchSpecialist); err != nil || sb.PendingPortChange.Approved {
		t.Fatalf("single sign should not approve: err=%v approved=%v", err, sb.PendingPortChange.Approved)
	}
	sb, err := dispatchSvc.SignPortChange(bk.ID, domain.SigDestPortAgent)
	if err != nil {
		t.Fatal(err)
	}
	// Both signatures present: the change is approved and applied, so the
	// pending request is cleared and the voyage destination is updated.
	if sb.PortChanges != 1 {
		t.Fatalf("want 1 change, got %d", sb.PortChanges)
	}
	if sb.PendingPortChange != nil {
		t.Fatal("pending port change should be cleared after apply")
	}
	v, _ := dispatchSvc.Get("V1")
	if v.DestPort != "HAM" {
		t.Fatalf("port not applied: %s", v.DestPort)
	}

	// Second change succeeds (cap is two).
	dispatchSvc.RequestPortChange(bk.ID, "ANT")
	dispatchSvc.SignPortChange(bk.ID, domain.SigDispatchSpecialist)
	dispatchSvc.SignPortChange(bk.ID, domain.SigDestPortAgent)
	if b2, _ := bookingSvc.Get(bk.ID); b2.PortChanges != 2 {
		t.Fatalf("want 2 changes, got %d", b2.PortChanges)
	}

	// Third change is blocked by the max-change rule.
	if _, err := dispatchSvc.RequestPortChange(bk.ID, "LEH"); !errors.Is(err, domain.ErrMaxPortChanges) {
		t.Fatalf("third change: want ErrMaxPortChanges, got %v", err)
	}

	// A voyage departing within 72h rejects any port change.
	mkVoyage(st, clk, "V2", 1, 0)
	st.Update(func(d *store.Data) error {
		d.Voyages["V2"].DepartureAt = clk.Now().Add(24 * time.Hour) // < 72h
		return nil
	})
	seedCargo(st, "C2", domain.CargoPowerBattery, false)
	bk2 := confirmBooking(t, bookingSvc, "C2", "V2", domain.SlotRefrigerated)
	if _, err := dispatchSvc.RequestPortChange(bk2.ID, "XYZ"); !errors.Is(err, domain.ErrPortChangeClosed) {
		t.Fatalf("window-closed: want ErrPortChangeClosed, got %v", err)
	}
}

func TestBerthChangePrioritisesPeakSeason(t *testing.T) {
	st, clk, bookingSvc, portappSvc, dispatchSvc := newDispatchEnv(t)
	mkVoyage(st, clk, "V1", 0, 2)
	// B is regular cargo (priority 10); A is peak-season new energy (priority 100).
	seedCargo(st, "CB", domain.CargoRegular, false)
	seedCargo(st, "CA", domain.CargoEnergyStorage, true)
	b := confirmBooking(t, bookingSvc, "CB", "V1", domain.SlotDry)
	a := confirmBooking(t, bookingSvc, "CA", "V1", domain.SlotDry)

	ws, we := clk.Now(), clk.Now().Add(time.Hour)
	apB, err := portappSvc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: b.ID, TruckID: "TB", WindowStart: ws, WindowEnd: we})
	if err != nil {
		t.Fatal(err)
	}
	apA, err := portappSvc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: a.ID, TruckID: "TA", WindowStart: ws, WindowEnd: we})
	if err != nil {
		t.Fatal(err)
	}
	// Before reorder: regular (B) is sequence 1, peak-season (A) is sequence 2.
	if apB.Sequence != 1 || apA.Sequence != 2 {
		t.Fatalf("initial sequences: B=%d A=%d", apB.Sequence, apA.Sequence)
	}

	if _, err := dispatchSvc.ChangeBerth("V1", "B2"); err != nil {
		t.Fatal(err)
	}
	apA2, _ := portappSvc.Get(apA.ID)
	apB2, _ := portappSvc.Get(apB.ID)
	// After berth change peak-season new energy is prioritised, regular postponed.
	if apA2.Sequence != 1 || apB2.Sequence != 2 {
		t.Fatalf("after reorder: want A=1 B=2, got A=%d B=%d", apA2.Sequence, apB2.Sequence)
	}
}

func TestShipDelayReappointsByPriority(t *testing.T) {
	st, clk, bookingSvc, portappSvc, dispatchSvc := newDispatchEnv(t)
	mkVoyage(st, clk, "V1", 0, 2)
	seedCargo(st, "CB", domain.CargoRegular, false)
	seedCargo(st, "CA", domain.CargoEnergyStorage, true)
	b := confirmBooking(t, bookingSvc, "CB", "V1", domain.SlotDry)
	a := confirmBooking(t, bookingSvc, "CA", "V1", domain.SlotDry)

	ws, we := clk.Now(), clk.Now().Add(time.Hour)
	apB, _ := portappSvc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: b.ID, TruckID: "TB", WindowStart: ws, WindowEnd: we})
	apA, _ := portappSvc.CreateAppointment(portapp.CreateAppointmentReq{BookingID: a.ID, TruckID: "TA", WindowStart: ws, WindowEnd: we})

	if _, err := dispatchSvc.MarkDelayed("V1"); err != nil {
		t.Fatal(err)
	}
	apA2, _ := portappSvc.Get(apA.ID)
	apB2, _ := portappSvc.Get(apB.ID)
	if apA2.Status != domain.AptRescheduled || apB2.Status != domain.AptRescheduled {
		t.Fatalf("want both rescheduled, got A=%s B=%s", apA2.Status, apB2.Status)
	}
	// Re-allocated by original priority: peak-season new energy first.
	if apA2.Sequence != 1 || apB2.Sequence != 2 {
		t.Fatalf("delay reorder: want A=1 B=2, got A=%d B=%d", apA2.Sequence, apB2.Sequence)
	}
}
