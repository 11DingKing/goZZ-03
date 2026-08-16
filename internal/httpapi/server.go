package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"arcticdispatch/internal/arrival"
	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/cargo"
	"arcticdispatch/internal/dispatch"
	"arcticdispatch/internal/domain"
	"arcticdispatch/internal/manifest"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/store"
)

// Server wires the domain services behind a REST surface.
type Server struct {
	store    *store.Store
	cargo    *cargo.Service
	booking  *booking.Service
	dispatch *dispatch.Service
	portapp  *portapp.Service
	manifest *manifest.Service
	arrival  *arrival.Service
	mux      *http.ServeMux
}

// New assembles a server with all services.
func New(st *store.Store, c *cargo.Service, b *booking.Service, d *dispatch.Service,
	pa *portapp.Service, m *manifest.Service, a *arrival.Service) *Server {
	s := &Server{
		store: st, cargo: c, booking: b, dispatch: d,
		portapp: pa, manifest: m, arrival: a, mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.health)

	s.mux.HandleFunc("POST /api/v1/cargo", s.createCargo)
	s.mux.HandleFunc("POST /api/v1/cargo/{id}/confirm-temp", s.confirmTemp)
	s.mux.HandleFunc("GET /api/v1/cargo/{id}", s.getCargo)

	s.mux.HandleFunc("POST /api/v1/voyages", s.createVoyage)
	s.mux.HandleFunc("GET /api/v1/voyages", s.listVoyages)
	s.mux.HandleFunc("GET /api/v1/voyages/{id}", s.getVoyage)
	s.mux.HandleFunc("POST /api/v1/voyages/{id}/delay", s.markDelayed)
	s.mux.HandleFunc("POST /api/v1/voyages/{id}/departed", s.markDeparted)
	s.mux.HandleFunc("POST /api/v1/voyages/{id}/arrived", s.markArrived)
	s.mux.HandleFunc("POST /api/v1/voyages/{id}/berth-change", s.changeBerth)

	s.mux.HandleFunc("POST /api/v1/bookings", s.createBooking)
	s.mux.HandleFunc("GET /api/v1/bookings/{id}", s.getBooking)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/pay", s.pay)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/cancel", s.cancelBooking)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/port-change", s.requestPortChange)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/port-change/sign", s.signPortChange)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/appointments", s.createAppointment)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/manifest", s.submitManifest)
	s.mux.HandleFunc("POST /api/v1/bookings/{id}/arrival", s.signArrival)

	s.mux.HandleFunc("POST /api/v1/manifests/{id}/declare", s.declareManifest)
	s.mux.HandleFunc("POST /api/v1/manifests/{id}/reject", s.rejectManifest)
}

// ServeHTTP routes the request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, mapError(err), map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func mapError(err error) int {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, domain.ErrInvalidState),
		errors.Is(err, domain.ErrTempControlFailed),
		errors.Is(err, domain.ErrPortChangeClosed),
		errors.Is(err, domain.ErrMaxPortChanges),
		errors.Is(err, domain.ErrPaymentTimeout):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrSlotUnavailable), errors.Is(err, domain.ErrInsufficientSlots):
		return http.StatusUnprocessableEntity
	}
	return http.StatusInternalServerError
}
