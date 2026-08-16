package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/store"
)

func TestStoreUpdatePersistAndRecover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(func(d *store.Data) error {
		d.Cargo["C1"] = &domain.Cargo{ID: "C1", Type: domain.CargoEnergyStorage, TempConfirmed: true}
		d.Voyages["V1"] = domain.NewVoyage("V1", "r", "SHA", "ROT", "B",
			time.Now(), time.Now().Add(7*24*time.Hour), 1, 0)
		d.Bookings["BK-1"] = &domain.Booking{ID: "BK-1", VoyageID: "V1", CargoID: "C1", Status: domain.BookingFrozen}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A fresh store opened on the same path must recover the aggregates.
	st2, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	var gotCargo *domain.Cargo
	var gotVoyage *domain.Voyage
	var gotBooking *domain.Booking
	st2.View(func(d *store.Data) {
		gotCargo = d.Cargo["C1"]
		gotVoyage = d.Voyages["V1"]
		gotBooking = d.Bookings["BK-1"]
	})
	if gotCargo == nil || gotCargo.Type != domain.CargoEnergyStorage {
		t.Fatalf("cargo not recovered: %+v", gotCargo)
	}
	if gotVoyage == nil || len(gotVoyage.Slots) != 1 {
		t.Fatalf("voyage not recovered: %+v", gotVoyage)
	}
	if gotBooking == nil || gotBooking.Status != domain.BookingFrozen {
		t.Fatalf("booking not recovered: %+v", gotBooking)
	}
}

func TestStoreRecoversFromCorruptSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(path)
	if err != nil {
		t.Fatalf("must boot despite corrupt snapshot, got %v", err)
	}
	var n int
	st.View(func(d *store.Data) { n = len(d.Cargo) })
	if n != 0 {
		t.Fatalf("expected empty store after corrupt load, got %d cargo", n)
	}
	if err := st.Snapshot(); err != nil {
		t.Fatal(err)
	}
	// The snapshot file should now be valid JSON and re-loadable.
	if _, err := store.New(path); err != nil {
		t.Fatalf("re-open after snapshot failed: %v", err)
	}
}

func TestStoreNextIDCounter(t *testing.T) {
	st := store.NewInMemory()
	var ids []string
	if err := st.Update(func(d *store.Data) error {
		ids = append(ids, store.NextID(d, "BK"), store.NextID(d, "BK"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ids[0] != "BK-1" || ids[1] != "BK-2" {
		t.Fatalf("want BK-1,BK-2 got %v", ids)
	}
	// A second store (in-memory) restarts the counter, as expected for a fresh
	// process with no snapshot.
	st2 := store.NewInMemory()
	var id string
	st2.Update(func(d *store.Data) error { id = store.NextID(d, "AP"); return nil })
	if id != "AP-1" {
		t.Fatalf("want AP-1 got %s", id)
	}
}
