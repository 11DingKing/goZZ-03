package domain

import "errors"

// Sentinel errors used across services and the HTTP layer. Services wrap these
// with fmt.Errorf("%w: ...", err) so the HTTP layer can map them to status codes
// while callers can inspect concrete reasons.
var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidState      = errors.New("invalid state transition")
	ErrSlotUnavailable   = errors.New("slot unavailable")
	ErrTempControlFailed = errors.New("temperature control validation failed")
	ErrPortChangeClosed  = errors.New("port change window closed")
	ErrMaxPortChanges    = errors.New("max port changes exceeded")
	ErrPaymentTimeout    = errors.New("payment timeout")
	ErrInsufficientSlots = errors.New("insufficient slots")
	ErrDuplicate         = errors.New("duplicate request")
)
