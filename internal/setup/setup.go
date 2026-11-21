// Package setup holds the game-neutral description of a setup file:
// a flat, ordered list of fields that can be compared.
package setup

import "time"

// Field is one comparable value of a setup, e.g. the front-left tyre pressure.
type Field struct {
	Path     string  `json:"path"`     // e.g. "basicSetup.tyres.tyrePressure[0]"
	Label    string  `json:"label"`    // e.g. "Tyre pressure FL"
	Section  string  `json:"section"`  // e.g. "Tyres"
	Value    string  `json:"value"`    // value as text, exactly as in the file
	Kind     string  `json:"kind"`     // "int", "float", "string", "bool" or "null"
	Num      float64 `json:"-"`        // numeric value when Kind is int or float
	Excluded bool    `json:"excluded"` // true = shown, but not used for the score
	// Real value as the game shows it (spec D-101, D-141): shown = Offset + clicks × Step, or
	// Table[clicks] when the game uses a lookup table (e.g. ACC caster), followed by Unit.
	// Real (or a Unit) means the value is converted; otherwise the clicks are shown.
	Real   bool      `json:"real,omitempty"`
	Unit   string    `json:"unit,omitempty"`
	Offset float64   `json:"offset,omitempty"`
	Step   float64   `json:"step,omitempty"`
	Table  []float64 `json:"table,omitempty"`
	// Choices names each click value where that is the same for every car, e.g. tyre compound
	// 0 = "Dry", 1 = "Wet" (D-120). Empty = show the number.
	Choices []string `json:"choices,omitempty"`
	// Signed: the game also stores values below zero for this setting (AC camber, e.g. -39), so
	// SetupMan lets it be changed below zero.
	Signed bool `json:"signed,omitempty"`
	// How the setup page shows the field, following the game's own setup screens (D-144):
	// Hidden = the game doesn't show it (listed only with "Also show hidden settings");
	// Group = a box within the section (e.g. "Pitstop strategy"); Row = the game's own name
	// for it (Label when empty); Col = the wheel or axle it belongs to (e.g. "Left front"),
	// so per-wheel settings are shown side by side as in the game.
	Hidden bool   `json:"hidden,omitempty"`
	Group  string `json:"group,omitempty"`
	Row    string `json:"row,omitempty"`
	Col    string `json:"col,omitempty"`
}

// IsNumber reports whether the field holds a number.
func (f Field) IsNumber() bool { return f.Kind == "int" || f.Kind == "float" }

// Converted reports whether the field has a real value (unit, offset/step or table), not only clicks.
func (f Field) Converted() bool { return f.IsNumber() && (f.Real || f.Unit != "") }

// RealValue is the value as the game shows it (the number itself when not converted).
func (f Field) RealValue() float64 {
	switch {
	case !f.Converted():
		return f.Num
	case len(f.Table) > 0:
		i := int(f.Num)
		if i < 0 {
			i = 0
		}
		if i >= len(f.Table) {
			i = len(f.Table) - 1
		}
		return f.Table[i]
	case f.Step == 0 && f.Offset == 0:
		return f.Num // stored in real units already
	default:
		step := f.Step
		if step == 0 {
			step = 1
		}
		return f.Offset + f.Num*step
	}
}

// Setup is one setup file found on disk.
type Setup struct {
	ID          string    `json:"id"`      // unique within one scan
	Name        string    `json:"name"`    // file name without extension
	RelPath     string    `json:"relPath"` // path relative to the game's setup folder
	Size        int64     `json:"size"`
	Modified    time.Time `json:"modified"`
	Fingerprint string    `json:"fingerprint"` // equal = identical (scored fields)
	Fields      []Field   `json:"fields"`
	Raw         []byte    `json:"-"` // the file content, stored as a version in the library
}
