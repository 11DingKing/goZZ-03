package domain

import "time"

// CargoType enumerates the cargo categories the dispatch centre handles.
type CargoType string

const (
	// CargoEnergyStorage is 储能柜 (energy storage cabinet).
	CargoEnergyStorage CargoType = "energy_storage"
	// CargoPowerBattery is 动力电池 (power battery).
	CargoPowerBattery CargoType = "power_battery"
	// CargoPVModule is 光伏组件 (photovoltaic module).
	CargoPVModule CargoType = "pv_module"
	// CargoRegular is 普通货物 (general cargo).
	CargoRegular CargoType = "regular"
)

// IsNewEnergy reports whether the cargo type belongs to the new-energy family
// that may occupy refrigerated slots when temperature control is confirmed.
func (c CargoType) IsNewEnergy() bool {
	switch c {
	case CargoEnergyStorage, CargoPowerBattery, CargoPVModule:
		return true
	default:
		return false
	}
}

// TemperatureClass is the temperature-control attribute a warehouse supervisor
// (仓储主管) confirms for a cargo before slot allocation.
type TemperatureClass string

const (
	// TempRefrigerated maps to 冷藏舱位 (refrigerated slot).
	TempRefrigerated TemperatureClass = "refrigerated"
	// TempDry maps to 干货舱位 (dry slot).
	TempDry TemperatureClass = "dry"
)

// Cargo is the cargo aggregate. Temperature confirmation is the gate that lets a
// new-energy cargo take a refrigerated slot.
type Cargo struct {
	ID            string           `json:"id"`
	Type          CargoType        `json:"type"`
	TempClass     TemperatureClass `json:"temp_class"`
	TempConfirmed bool             `json:"temp_confirmed"`
	ConfirmedAt   time.Time        `json:"confirmed_at,omitempty"`
	OwnerID       string           `json:"owner_id"`
	PeakSeason    bool             `json:"peak_season"`
}

// EligibleForSlot reports whether the cargo is allowed to occupy the given slot
// type. Refrigerated slots are reserved for temperature-confirmed new-energy
// cargo (储能柜, 动力电池, 光伏组件); dry slots accept any cargo.
func (c Cargo) EligibleForSlot(slotType SlotType) bool {
	switch slotType {
	case SlotRefrigerated:
		return c.TempConfirmed && c.Type.IsNewEnergy()
	case SlotDry:
		return true
	default:
		return false
	}
}

// Priority computes the dispatch priority of a cargo: peak-season new-energy
// orders rank highest, other new-energy cargo next, general cargo last.
func (c Cargo) Priority() int {
	switch {
	case c.PeakSeason && c.Type.IsNewEnergy():
		return 100
	case c.Type.IsNewEnergy():
		return 50
	default:
		return 10
	}
}
