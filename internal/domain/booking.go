package domain

import (
	"fmt"
	"time"
)

// BookingStatus tracks the five-step chained flow:
// 订舱 → 配额冻结 → 进港预约 → 舱单申报 → 到港签收.
type BookingStatus string

const (
	// BookingPending: created, awaiting slot allocation (订舱).
	BookingPending BookingStatus = "pending"
	// BookingFrozen: slot frozen, awaiting payment (配额冻结).
	BookingFrozen BookingStatus = "frozen"
	// BookingConfirmed: paid and slot allocated; ready for 进港预约 + 舱单申报.
	BookingConfirmed BookingStatus = "confirmed"
	// BookingManifested: 舱单申报 declared.
	BookingManifested BookingStatus = "manifested"
	// BookingArrived: 到港签收 completed.
	BookingArrived BookingStatus = "arrived"
	// BookingWaitlisted: no slot available, queued for promotion (候补).
	BookingWaitlisted BookingStatus = "waitlisted"
	// BookingReleased: frozen slot released after payment timeout.
	BookingReleased BookingStatus = "released"
	// BookingCancelled: cancelled.
	BookingCancelled BookingStatus = "cancelled"
)

// Business rule constants.
const (
	// MaxPortChanges: a single booking may change port at most twice.
	MaxPortChanges = 2
	// PortChangeWindow: no port change later than 72h before departure.
	PortChangeWindow = 72 * time.Hour
	// PaymentTimeout: a frozen slot auto-releases after 10 minutes without payment.
	PaymentTimeout = 10 * time.Minute
)

// SignatureRole enumerates the two roles required for dual-sign port changes.
type SignatureRole string

const (
	SigDispatchSpecialist SignatureRole = "dispatch_specialist" // 调度专员
	SigDestPortAgent      SignatureRole = "dest_port_agent"     // 目的港代理
)

// PortChangeRequest records a pending dual-signature port change.
type PortChangeRequest struct {
	NewPort      string    `json:"new_port"`
	DispatchSign bool      `json:"dispatch_sign"`
	AgentSign    bool      `json:"agent_sign"`
	RequestedAt  time.Time `json:"requested_at"`
	ApprovedAt   time.Time `json:"approved_at,omitempty"`
	Approved     bool      `json:"approved"`
}

// Booking is the central aggregate binding cargo, voyage, slot and flow state.
type Booking struct {
	ID                string             `json:"id"`
	IdempotencyKey    string             `json:"idempotency_key,omitempty"`
	SalesManagerID    string             `json:"sales_manager_id"`
	CargoID           string             `json:"cargo_id"`
	VoyageID          string             `json:"voyage_id"`
	SlotType          SlotType           `json:"slot_type"`
	Quantity          int                `json:"quantity"`
	Status            BookingStatus      `json:"status"`
	SubmitTime        time.Time          `json:"submit_time"`
	FrozenAt          time.Time          `json:"frozen_at,omitempty"`
	PaidAt            time.Time          `json:"paid_at,omitempty"`
	SlotIndex         int                `json:"slot_index"`
	PortChanges       int                `json:"port_changes"`
	PendingPortChange *PortChangeRequest `json:"pending_port_change,omitempty"`
	Priority          int                `json:"priority"`
}

// NewBooking constructs a booking, deriving priority from the cargo.
func NewBooking(id, key, sales, cargoID, voyageID string, slotType SlotType, qty int, now time.Time, cargo *Cargo) *Booking {
	prio := 10
	if cargo != nil {
		prio = cargo.Priority()
	}
	return &Booking{
		ID:             id,
		IdempotencyKey: key,
		SalesManagerID: sales,
		CargoID:        cargoID,
		VoyageID:       voyageID,
		SlotType:       slotType,
		Quantity:       qty,
		Status:         BookingPending,
		SubmitTime:     now,
		SlotIndex:      -1,
		Priority:       prio,
	}
}

// ValidatePortChange checks the business rules for requesting a port change:
// valid status, under the max-change count, within the 72h window, and no other
// pending change outstanding.
func (b *Booking) ValidatePortChange(voyage *Voyage, now time.Time) error {
	switch b.Status {
	case BookingFrozen, BookingConfirmed, BookingManifested:
	default:
		return fmt.Errorf("%w: booking %s status %s cannot change port", ErrInvalidState, b.ID, b.Status)
	}
	if b.PortChanges >= MaxPortChanges {
		return fmt.Errorf("%w: booking %s already used %d port changes", ErrMaxPortChanges, b.ID, b.PortChanges)
	}
	if !voyage.CanChangePort(now) {
		return fmt.Errorf("%w: voyage %s deadline %s passed", ErrPortChangeClosed, voyage.ID, voyage.PortChangeDeadline())
	}
	if b.PendingPortChange != nil && !b.PendingPortChange.Approved {
		return fmt.Errorf("%w: booking %s has a pending port change", ErrConflict, b.ID)
	}
	return nil
}

// RequestPortChange starts a dual-signature port change request.
func (b *Booking) RequestPortChange(newPort string, now time.Time) {
	b.PendingPortChange = &PortChangeRequest{
		NewPort:     newPort,
		RequestedAt: now,
	}
}

// SignPortChange records a signature for a pending port change. When both
// signatures are present the change is approved.
func (b *Booking) SignPortChange(role SignatureRole, now time.Time) error {
	if b.PendingPortChange == nil {
		return fmt.Errorf("%w: no pending port change for booking %s", ErrNotFound, b.ID)
	}
	switch role {
	case SigDispatchSpecialist:
		b.PendingPortChange.DispatchSign = true
	case SigDestPortAgent:
		b.PendingPortChange.AgentSign = true
	default:
		return fmt.Errorf("unknown signature role %q", role)
	}
	if b.PendingPortChange.DispatchSign && b.PendingPortChange.AgentSign {
		b.PendingPortChange.Approved = true
		b.PendingPortChange.ApprovedAt = now
		b.PortChanges++
	}
	return nil
}

// ApplyApprovedPortChange finalises an approved port change on the voyage. It
// returns false if no approved change is pending.
func (b *Booking) ApplyApprovedPortChange(voyage *Voyage) bool {
	if b.PendingPortChange == nil || !b.PendingPortChange.Approved {
		return false
	}
	voyage.DestPort = b.PendingPortChange.NewPort
	b.PendingPortChange = nil
	return true
}
