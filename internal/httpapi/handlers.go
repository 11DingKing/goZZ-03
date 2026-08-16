package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/cargo"
	"arcticdispatch/internal/dispatch"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/manifest"
	"arcticdispatch/internal/portapp"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createCargo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		TempClass  string `json:"temp_class"`
		OwnerID    string `json:"owner_id"`
		PeakSeason bool   `json:"peak_season"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	c, err := s.cargo.Create(cargo.CreateReq{
		ID: req.ID, Type: domain.CargoType(req.Type),
		TempClass: domain.TemperatureClass(req.TempClass),
		OwnerID:   req.OwnerID, PeakSeason: req.PeakSeason,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) confirmTemp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.cargo.ConfirmTemp(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) getCargo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, ok := s.cargo.Get(id)
	if !ok {
		writeError(w, notFoundErr("cargo", id))
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) createVoyage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID                string `json:"id"`
		Route             string `json:"route"`
		OriginPort        string `json:"origin_port"`
		DestPort          string `json:"dest_port"`
		Berth             string `json:"berth"`
		DepartureAt       string `json:"departure_at"`
		ArrivalAt         string `json:"arrival_at"`
		RefrigeratedSlots int    `json:"refrigerated_slots"`
		DrySlots          int    `json:"dry_slots"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	dep, err := time.Parse(time.RFC3339, req.DepartureAt)
	if err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, errors.New("invalid departure_at")))
		return
	}
	arr, err := time.Parse(time.RFC3339, req.ArrivalAt)
	if err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, errors.New("invalid arrival_at")))
		return
	}
	v, err := s.dispatch.CreateVoyage(dispatch.CreateVoyageReq{
		ID: req.ID, Route: req.Route, OriginPort: req.OriginPort, DestPort: req.DestPort,
		Berth: req.Berth, DepartureAt: dep, ArrivalAt: arr,
		RefrigeratedSlots: req.RefrigeratedSlots, DrySlots: req.DrySlots,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) listVoyages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.dispatch.List())
}

func (s *Server) getVoyage(w http.ResponseWriter, r *http.Request) {
	v, ok := s.dispatch.Get(r.PathValue("id"))
	if !ok {
		writeError(w, notFoundErr("voyage", r.PathValue("id")))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) markDelayed(w http.ResponseWriter, r *http.Request) {
	v, err := s.dispatch.MarkDelayed(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) markDeparted(w http.ResponseWriter, r *http.Request) {
	v, err := s.dispatch.MarkDeparted(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) markArrived(w http.ResponseWriter, r *http.Request) {
	v, err := s.dispatch.MarkArrived(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) changeBerth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Berth string `json:"berth"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	v, err := s.dispatch.ChangeBerth(r.PathValue("id"), req.Berth)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
		SalesManagerID string `json:"sales_manager_id"`
		CargoID        string `json:"cargo_id"`
		VoyageID       string `json:"voyage_id"`
		SlotType       string `json:"slot_type"`
		Quantity       int    `json:"quantity"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	bk, err := s.booking.CreateBooking(booking.CreateBookingReq{
		IdempotencyKey: req.IdempotencyKey, SalesManagerID: req.SalesManagerID,
		CargoID: req.CargoID, VoyageID: req.VoyageID,
		SlotType: domain.SlotType(req.SlotType), Quantity: req.Quantity,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if bk.Status == domain.BookingWaitlisted {
		writeJSON(w, http.StatusAccepted, bk)
		return
	}
	writeJSON(w, http.StatusCreated, bk)
}

func (s *Server) getBooking(w http.ResponseWriter, r *http.Request) {
	bk, ok := s.booking.Get(r.PathValue("id"))
	if !ok {
		writeError(w, notFoundErr("booking", r.PathValue("id")))
		return
	}
	writeJSON(w, http.StatusOK, bk)
}

func (s *Server) pay(w http.ResponseWriter, r *http.Request) {
	bk, err := s.booking.Pay(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bk)
}

func (s *Server) cancelBooking(w http.ResponseWriter, r *http.Request) {
	bk, err := s.booking.Cancel(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bk)
}

func (s *Server) requestPortChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NewPort string `json:"new_port"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	bk, err := s.dispatch.RequestPortChange(r.PathValue("id"), req.NewPort)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, bk)
}

func (s *Server) signPortChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	bk, err := s.dispatch.SignPortChange(r.PathValue("id"), domain.SignatureRole(req.Role))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bk)
}

func (s *Server) createAppointment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TruckID     string `json:"truck_id"`
		WindowStart string `json:"window_start"`
		WindowEnd   string `json:"window_end"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	ws, err := time.Parse(time.RFC3339, req.WindowStart)
	if err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, errors.New("invalid window_start")))
		return
	}
	we, err := time.Parse(time.RFC3339, req.WindowEnd)
	if err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, errors.New("invalid window_end")))
		return
	}
	apt, err := s.portapp.CreateAppointment(portapp.CreateAppointmentReq{
		BookingID: r.PathValue("id"), TruckID: req.TruckID,
		WindowStart: ws, WindowEnd: we,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apt)
}

func (s *Server) submitManifest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CargoDesc  string  `json:"cargo_desc"`
		WeightTons float64 `json:"weight_tons"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	m, err := s.manifest.Submit(manifest.SubmitReq{
		BookingID: r.PathValue("id"), CargoDesc: req.CargoDesc, WeightTons: req.WeightTons,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) declareManifest(w http.ResponseWriter, r *http.Request) {
	m, err := s.manifest.Declare(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) rejectManifest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	m, err := s.manifest.Reject(r.PathValue("id"), req.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) signArrival(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SignedBy string `json:"signed_by"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, errors.Join(domain.ErrInvalidState, err))
		return
	}
	ar, err := s.arrival.SignOff(r.PathValue("id"), req.SignedBy)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ar)
}

// notFoundErr returns a wrapped ErrNotFound for the HTTP error mapper.
func notFoundErr(what, id string) error {
	return errNotFound{what: what, id: id}
}

type errNotFound struct{ what, id string }

func (e errNotFound) Error() string { return e.what + " " + e.id + " not found" }

func (e errNotFound) Is(target error) bool { return target == domain.ErrNotFound }

// decodeJSON is exported for tests that need to decode a manifest response body.
func decodeJSON(b []byte, v any) error { return json.Unmarshal(b, v) }
