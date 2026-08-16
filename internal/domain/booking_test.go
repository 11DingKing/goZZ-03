package domain_test

import (
	"errors"
	"testing"
	"time"

	"arcticdispatch/internal/domain"
)

func TestCargoEligibilityForRefrigeratedSlot(t *testing.T) {
	cases := []struct {
		name  string
		cargo domain.Cargo
		slot  domain.SlotType
		want  bool
	}{
		{"confirmed energy storage on refrigerated", domain.Cargo{Type: domain.CargoEnergyStorage, TempConfirmed: true}, domain.SlotRefrigerated, true},
		{"unconfirmed energy storage on refrigerated", domain.Cargo{Type: domain.CargoEnergyStorage}, domain.SlotRefrigerated, false},
		{"power battery on refrigerated", domain.Cargo{Type: domain.CargoPowerBattery, TempConfirmed: true}, domain.SlotRefrigerated, true},
		{"pv module on refrigerated", domain.Cargo{Type: domain.CargoPVModule, TempConfirmed: true}, domain.SlotRefrigerated, true},
		{"regular cargo on refrigerated", domain.Cargo{Type: domain.CargoRegular, TempConfirmed: true}, domain.SlotRefrigerated, false},
		{"regular cargo on dry", domain.Cargo{Type: domain.CargoRegular}, domain.SlotDry, true},
		{"pv module on dry", domain.Cargo{Type: domain.CargoPVModule}, domain.SlotDry, true},
	}
	for _, c := range cases {
		if got := c.cargo.EligibleForSlot(c.slot); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if got := (domain.Cargo{Type: domain.CargoEnergyStorage, PeakSeason: true}).Priority(); got != 100 {
		t.Errorf("peak new-energy priority = %d want 100", got)
	}
	if got := (domain.Cargo{Type: domain.CargoPVModule}).Priority(); got != 50 {
		t.Errorf("new-energy priority = %d want 50", got)
	}
	if got := (domain.Cargo{Type: domain.CargoRegular}).Priority(); got != 10 {
		t.Errorf("regular priority = %d want 10", got)
	}
}

func TestVoyageSlotFreezeAllocateRelease(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v := domain.NewVoyage("V1", "route", "SHA", "ROT", "B1",
		now.Add(7*24*time.Hour), now.Add(14*24*time.Hour), 2, 1)
	if len(v.FreeSlots(domain.SlotRefrigerated)) != 2 {
		t.Fatalf("expected 2 free refrigerated slots, got %d", len(v.FreeSlots(domain.SlotRefrigerated)))
	}
	if v.Slots[2].Type != domain.SlotDry {
		t.Fatalf("expected dry slot at index 2, got %s", v.Slots[2].Type)
	}
	if err := v.FreezeSlot(0, "BK-1", now); err != nil {
		t.Fatal(err)
	}
	if v.Slots[0].Status != domain.SlotFrozen {
		t.Fatal("slot not frozen")
	}
	if err := v.FreezeSlot(0, "BK-2", now); err == nil {
		t.Fatal("expected error double-freezing slot")
	}
	if err := v.AllocateSlot(0, "BK-1"); err != nil {
		t.Fatal(err)
	}
	if v.Slots[0].Status != domain.SlotAllocated {
		t.Fatal("slot not allocated")
	}
	idx, ok := v.SlotByBooking("BK-1")
	if !ok || idx != 0 {
		t.Fatalf("SlotByBooking = %d,%v want 0,true", idx, ok)
	}
	v.ReleaseSlot(0)
	if v.Slots[0].Status != domain.SlotFree {
		t.Fatal("slot not released")
	}
}

func TestBookingPortChangeValidationRules(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v := domain.NewVoyage("V1", "r", "SHA", "ROT", "B1",
		now.Add(10*24*time.Hour), now.Add(20*24*time.Hour), 1, 0)
	bk := domain.NewBooking("BK-1", "", "S1", "C1", "V1", domain.SlotRefrigerated, 1, now, nil)

	bk.Status = domain.BookingPending
	if err := bk.ValidatePortChange(v, now); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("pending: want ErrInvalidState, got %v", err)
	}

	bk.Status = domain.BookingFrozen
	if err := bk.ValidatePortChange(v, now); err != nil {
		t.Fatalf("frozen within window: want nil, got %v", err)
	}

	// Window closes 72h before departure; one second past it must be rejected.
	late := v.PortChangeDeadline().Add(time.Second)
	if err := bk.ValidatePortChange(v, late); !errors.Is(err, domain.ErrPortChangeClosed) {
		t.Fatalf("past deadline: want ErrPortChangeClosed, got %v", err)
	}
}

func TestPortChangeDualSignatureApproval(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v := domain.NewVoyage("V1", "r", "SHA", "ROT", "B1",
		now.Add(10*24*time.Hour), now.Add(20*24*time.Hour), 1, 0)
	bk := domain.NewBooking("BK-1", "", "S1", "C1", "V1", domain.SlotRefrigerated, 1, now, nil)
	bk.Status = domain.BookingConfirmed

	bk.RequestPortChange("HAM", now)
	if bk.PendingPortChange == nil || bk.PendingPortChange.Approved {
		t.Fatal("pending request not set or already approved")
	}
	if err := bk.SignPortChange(domain.SigDispatchSpecialist, now); err != nil {
		t.Fatal(err)
	}
	if bk.PendingPortChange.Approved {
		t.Fatal("single signature should not approve")
	}
	if err := bk.SignPortChange(domain.SigDestPortAgent, now); err != nil {
		t.Fatal(err)
	}
	if !bk.PendingPortChange.Approved || bk.PortChanges != 1 {
		t.Fatalf("want approved + 1 change, got approved=%v changes=%d", bk.PendingPortChange.Approved, bk.PortChanges)
	}
	if !bk.ApplyApprovedPortChange(v) || v.DestPort != "HAM" {
		t.Fatalf("port change not applied, dest=%s", v.DestPort)
	}
	if bk.PendingPortChange != nil {
		t.Fatal("pending request should be cleared after apply")
	}

	// Second change (cap is two).
	bk.RequestPortChange("ANT", now)
	bk.SignPortChange(domain.SigDispatchSpecialist, now)
	bk.SignPortChange(domain.SigDestPortAgent, now)
	bk.ApplyApprovedPortChange(v)
	if bk.PortChanges != 2 {
		t.Fatalf("want 2 changes, got %d", bk.PortChanges)
	}

	// Third change must be blocked by the max-change rule.
	bk.RequestPortChange("LEH", now)
	if err := bk.ValidatePortChange(v, now); !errors.Is(err, domain.ErrMaxPortChanges) {
		t.Fatalf("third change: want ErrMaxPortChanges, got %v", err)
	}
}
