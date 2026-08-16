package domain

import "time"

// AppointmentStatus tracks 进港预约 (port-arrival appointment) state.
type AppointmentStatus string

const (
	AptScheduled    AppointmentStatus = "scheduled"
	AptReScheduling AppointmentStatus = "re_scheduling"
	AptRescheduled  AppointmentStatus = "rescheduled"
	AptCompleted    AppointmentStatus = "completed"
	AptCancelled    AppointmentStatus = "cancelled"
)

// PortAppointment binds a booking to a truck pickup window at the port.
type PortAppointment struct {
	ID          string            `json:"id"`
	BookingID   string            `json:"booking_id"`
	VoyageID    string            `json:"voyage_id"`
	TruckID     string            `json:"truck_id"`
	WindowStart time.Time         `json:"window_start"`
	WindowEnd   time.Time         `json:"window_end"`
	Status      AppointmentStatus `json:"status"`
	Sequence    int               `json:"sequence"`
	Priority    int               `json:"priority"`
	CreatedAt   time.Time         `json:"created_at"`
}

// ManifestStatus tracks 舱单申报 (manifest declaration) state.
type ManifestStatus string

const (
	ManifestDraft     ManifestStatus = "draft"
	ManifestSubmitted ManifestStatus = "submitted"
	ManifestDeclared  ManifestStatus = "declared"
	ManifestRejected  ManifestStatus = "rejected"
)

// Manifest carries the declaration data for a booking.
type Manifest struct {
	ID          string         `json:"id"`
	BookingID   string         `json:"booking_id"`
	VoyageID    string         `json:"voyage_id"`
	CargoDesc   string         `json:"cargo_desc"`
	WeightTons  float64        `json:"weight_tons"`
	Status      ManifestStatus `json:"status"`
	SubmittedAt time.Time      `json:"submitted_at,omitempty"`
	DeclaredAt  time.Time      `json:"declared_at,omitempty"`
	RejectedAt  time.Time      `json:"rejected_at,omitempty"`
	Rejection   string         `json:"rejection,omitempty"`
}

// ArrivalStatus tracks 到港签收 (arrival sign-off) state.
type ArrivalStatus string

const (
	ArrivalPending ArrivalStatus = "pending"
	ArrivalSigned  ArrivalStatus = "signed"
)

// Arrival records the sign-off of a booking at the destination port.
type Arrival struct {
	ID        string        `json:"id"`
	BookingID string        `json:"booking_id"`
	VoyageID  string        `json:"voyage_id"`
	Status    ArrivalStatus `json:"status"`
	SignedAt  time.Time     `json:"signed_at,omitempty"`
	SignedBy  string        `json:"signed_by,omitempty"`
}
