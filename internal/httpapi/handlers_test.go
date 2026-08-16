package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"arcticdispatch/internal/arrival"
	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/cargo"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/dispatch"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/httpapi"
	"arcticdispatch/internal/manifest"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st := store.NewInMemory()
	clk := clock.System{}
	cargoSvc := cargo.New(st, clk)
	bookingSvc := booking.New(st, clk, domain.PaymentTimeout)
	portappSvc := portapp.New(st, clk)
	dispatchSvc := dispatch.New(st, clk, portappSvc)
	manifestSvc := manifest.New(st, clk)
	arrivalSvc := arrival.New(st, clk)
	srv := httpapi.New(st, cargoSvc, bookingSvc, dispatchSvc, portappSvc, manifestSvc, arrivalSvc)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts, st
}

func postJSON(t *testing.T, c *http.Client, url string, body any) (int, []byte) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func getJSON(t *testing.T, c *http.Client, url string) (int, []byte) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func futureDeparture() string {
	return time.Now().Add(10 * 24 * time.Hour).UTC().Format(time.RFC3339)
}

func TestHTTPFullChainedFlow(t *testing.T) {
	ts, _ := newServer(t)
	c := ts.Client()

	// Register and temperature-confirm new-energy cargo.
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/cargo", map[string]any{"id": "C1", "type": "energy_storage", "temp_class": "refrigerated", "owner_id": "O1", "peak_season": true}); code != 201 {
		t.Fatalf("create cargo: %d", code)
	}
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/cargo/C1/confirm-temp", map[string]any{}); code != 200 {
		t.Fatalf("confirm temp: %d", code)
	}
	// Provision a voyage with one refrigerated slot.
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/voyages", map[string]any{"id": "V1", "origin_port": "SHA", "dest_port": "ROT", "berth": "B1", "departure_at": futureDeparture(), "arrival_at": futureDeparture(), "refrigerated_slots": 1, "dry_slots": 0}); code != 201 {
		t.Fatalf("create voyage: %d", code)
	}
	// 订舱 -> frozen.
	code, body := postJSON(t, c, ts.URL+"/api/v1/bookings", map[string]any{"idempotency_key": "K1", "sales_manager_id": "S1", "cargo_id": "C1", "voyage_id": "V1", "slot_type": "refrigerated", "quantity": 1})
	if code != 201 {
		t.Fatalf("create booking: %d %s", code, body)
	}
	var bk domain.Booking
	if err := json.Unmarshal(body, &bk); err != nil {
		t.Fatal(err)
	}
	if bk.Status != domain.BookingFrozen {
		t.Fatalf("want frozen, got %s", bk.Status)
	}
	// 配额冻结 -> pay -> confirmed.
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/bookings/"+bk.ID+"/pay", map[string]any{}); code != 200 {
		t.Fatalf("pay: %d", code)
	}
	// 进港预约.
	ws := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	we := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/bookings/"+bk.ID+"/appointments", map[string]any{"truck_id": "T1", "window_start": ws, "window_end": we}); code != 201 {
		t.Fatalf("appointment: %d", code)
	}
	// 舱单申报 submit + declare.
	code, body = postJSON(t, c, ts.URL+"/api/v1/bookings/"+bk.ID+"/manifest", map[string]any{"cargo_desc": "batteries", "weight_tons": 12.5})
	if code != 201 {
		t.Fatalf("submit manifest: %d %s", code, body)
	}
	var mf domain.Manifest
	json.Unmarshal(body, &mf)
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/manifests/"+mf.ID+"/declare", map[string]any{}); code != 200 {
		t.Fatalf("declare manifest: %d", code)
	}
	// Mark voyage arrived, then 到港签收.
	if code, _ := postJSON(t, c, ts.URL+"/api/v1/voyages/V1/arrived", map[string]any{}); code != 200 {
		t.Fatalf("arrived: %d", code)
	}
	if code, body := postJSON(t, c, ts.URL+"/api/v1/bookings/"+bk.ID+"/arrival", map[string]any{"signed_by": "agent"}); code != 200 {
		t.Fatalf("arrival: %d %s", code, body)
	}
	// Final state: booking arrived.
	code, body = getJSON(t, c, ts.URL+"/api/v1/bookings/"+bk.ID)
	if code != http.StatusOK {
		t.Fatalf("get booking: %d", code)
	}
	json.Unmarshal(body, &bk)
	if bk.Status != domain.BookingArrived {
		t.Fatalf("want arrived, got %s", bk.Status)
	}
}

func TestHTTPRefrigeratedTempControlRejection(t *testing.T) {
	ts, _ := newServer(t)
	c := ts.Client()

	postJSON(t, c, ts.URL+"/api/v1/cargo", map[string]any{"id": "CR", "type": "regular", "temp_class": "dry", "owner_id": "O1"})
	postJSON(t, c, ts.URL+"/api/v1/cargo/CR/confirm-temp", map[string]any{})
	postJSON(t, c, ts.URL+"/api/v1/voyages", map[string]any{"id": "V1", "origin_port": "SHA", "dest_port": "ROT", "berth": "B1", "departure_at": futureDeparture(), "arrival_at": futureDeparture(), "refrigerated_slots": 1, "dry_slots": 0})

	// Regular cargo cannot occupy a refrigerated slot -> 400.
	code, body := postJSON(t, c, ts.URL+"/api/v1/bookings", map[string]any{"sales_manager_id": "S1", "cargo_id": "CR", "voyage_id": "V1", "slot_type": "refrigerated", "quantity": 1})
	if code != 400 {
		t.Fatalf("want 400 for temp-control failure, got %d %s", code, body)
	}
	var e map[string]string
	json.Unmarshal(body, &e)
	if e["error"] == "" {
		t.Fatal("expected error message")
	}
}
