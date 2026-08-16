package domain

import (
	"fmt"
	"time"
)

// SlotType distinguishes refrigerated and dry cabin slots.
type SlotType string

const (
	SlotRefrigerated SlotType = "refrigerated"
	SlotDry          SlotType = "dry"
)

// SlotStatus tracks a slot's lifecycle.
type SlotStatus string

const (
	SlotFree      SlotStatus = "free"
	SlotFrozen    SlotStatus = "frozen"
	SlotAllocated SlotStatus = "allocated"
)

// Slot is a single cabin slot owned by a voyage.
type Slot struct {
	VoyageID  string     `json:"voyage_id"`
	Index     int        `json:"index"`
	Type      SlotType   `json:"type"`
	Status    SlotStatus `json:"status"`
	BookingID string     `json:"booking_id,omitempty"`
	FrozenAt  time.Time  `json:"frozen_at,omitempty"`
}

// VoyageStatus tracks a voyage's operational state.
type VoyageStatus string

const (
	VoyageScheduled VoyageStatus = "scheduled"
	VoyageDelayed   VoyageStatus = "delayed"
	VoyageDeparted  VoyageStatus = "departed"
	VoyageArrived   VoyageStatus = "arrived"
)

// Voyage is the sailing aggregate that owns slots and a berth schedule.
type Voyage struct {
	ID          string       `json:"id"`
	Route       string       `json:"route"`
	OriginPort  string       `json:"origin_port"`
	DestPort    string       `json:"dest_port"`
	Berth       string       `json:"berth"`
	DepartureAt time.Time    `json:"departure_at"`
	ArrivalAt   time.Time    `json:"arrival_at"`
	Status      VoyageStatus `json:"status"`
	Slots       []Slot       `json:"slots"`
}

// NewVoyage builds a voyage with nRefrig refrigerated and nDry dry slots.
func NewVoyage(id, route, origin, dest, berth string, dep, arr time.Time, nRefrig, nDry int) *Voyage {
	v := &Voyage{
		ID: id, Route: route, OriginPort: origin, DestPort: dest, Berth: berth,
		DepartureAt: dep, ArrivalAt: arr, Status: VoyageScheduled,
	}
	idx := 0
	for i := 0; i < nRefrig; i++ {
		v.Slots = append(v.Slots, Slot{VoyageID: id, Index: idx, Type: SlotRefrigerated, Status: SlotFree})
		idx++
	}
	for i := 0; i < nDry; i++ {
		v.Slots = append(v.Slots, Slot{VoyageID: id, Index: idx, Type: SlotDry, Status: SlotFree})
		idx++
	}
	return v
}

// PortChangeDeadline is the latest time a port change may be requested: 72h
// before departure.
func (v *Voyage) PortChangeDeadline() time.Time {
	return v.DepartureAt.Add(-PortChangeWindow)
}

// CanChangePort reports whether a port change is still within the allowed window.
func (v *Voyage) CanChangePort(now time.Time) bool {
	return now.Before(v.PortChangeDeadline())
}

// FreezeSlot marks a free slot as frozen for a booking.
func (v *Voyage) FreezeSlot(index int, bookingID string, now time.Time) error {
	if index < 0 || index >= len(v.Slots) {
		return fmt.Errorf("slot index %d out of range", index)
	}
	s := &v.Slots[index]
	if s.Status != SlotFree {
		return fmt.Errorf("slot %d on voyage %s is %s, not free", index, v.ID, s.Status)
	}
	s.Status = SlotFrozen
	s.BookingID = bookingID
	s.FrozenAt = now
	return nil
}

// AllocateSlot promotes a frozen slot to allocated (payment received).
func (v *Voyage) AllocateSlot(index int, bookingID string) error {
	if index < 0 || index >= len(v.Slots) {
		return fmt.Errorf("slot index %d out of range", index)
	}
	s := &v.Slots[index]
	if s.Status != SlotFrozen || s.BookingID != bookingID {
		return fmt.Errorf("slot %d not frozen for booking %s", index, bookingID)
	}
	s.Status = SlotAllocated
	return nil
}

// ReleaseSlot returns a slot to the free pool (e.g. after payment timeout).
func (v *Voyage) ReleaseSlot(index int) {
	if index < 0 || index >= len(v.Slots) {
		return
	}
	s := &v.Slots[index]
	s.Status = SlotFree
	s.BookingID = ""
	s.FrozenAt = time.Time{}
}

// SlotByBooking returns the index of the slot currently held by a booking.
func (v *Voyage) SlotByBooking(bookingID string) (int, bool) {
	for i := range v.Slots {
		if v.Slots[i].BookingID == bookingID && v.Slots[i].Status != SlotFree {
			return i, true
		}
	}
	return -1, false
}

// FreeSlots returns the indexes of free slots of the requested type.
func (v *Voyage) FreeSlots(slotType SlotType) []int {
	var idx []int
	for i := range v.Slots {
		if v.Slots[i].Type == slotType && v.Slots[i].Status == SlotFree {
			idx = append(idx, i)
		}
	}
	return idx
}
