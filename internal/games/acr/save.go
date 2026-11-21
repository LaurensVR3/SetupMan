package acr

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
	"unicode/utf16"
)

// AC Rally keeps every setup in one Unreal Engine save file (SaveGames\CarSetupsDataSaveSlot.sav).
// After the standard header comes "CarSetupsSaveGameData", the data size, and then:
//
//	int32 cars; per car: string carID, int32 setups; per setup (a "record"):
//	  text name, int32 (unknown, 0), int64 saved (.NET ticks), string gameVersion, string carID,
//	  string track, []string surfaces, []string weathers, []string tags,
//	  int32 n, n × (string key, string value)       e.g. "Wheels.FrontLeft.TyrePressure" = "32.000000"
//
// Strings are Unreal strings: int32 length (with the closing zero; negative = UTF-16), then the text.
// Worked out from a real save of version 0.2.2 (D-111).

var errFormat = errors.New("not an AC Rally setups save file")

// Record is one setup in the save file.
type Record struct {
	Name     string
	Saved    time.Time
	Version  string
	Car      string
	Track    string
	Surfaces []string
	Weathers []string
	Tags     []string
	Values   [][2]string // key, value in file order
	Raw      []byte      // the record's own bytes (stored in the library as the setup's content)
}

type reader struct {
	b   []byte
	i   int
	err error
}

func (r *reader) i32() int32 {
	if r.err != nil || r.i+4 > len(r.b) {
		r.err = errFormat
		return 0
	}
	v := int32(binary.LittleEndian.Uint32(r.b[r.i:]))
	r.i += 4
	return v
}

func (r *reader) i64() int64 {
	if r.err != nil || r.i+8 > len(r.b) {
		r.err = errFormat
		return 0
	}
	v := int64(binary.LittleEndian.Uint64(r.b[r.i:]))
	r.i += 8
	return v
}

func (r *reader) str() string {
	n := int(r.i32())
	if r.err != nil {
		return ""
	}
	switch {
	case n == 0:
		return ""
	case n > 0 && n <= 4096 && r.i+n <= len(r.b):
		s := r.b[r.i : r.i+n-1]
		r.i += n
		return string(s)
	case n < 0 && -n <= 4096 && r.i-2*n <= len(r.b):
		u := make([]uint16, -n-1)
		for k := range u {
			u[k] = binary.LittleEndian.Uint16(r.b[r.i+2*k:])
		}
		r.i += -2 * n
		return string(utf16.Decode(u))
	}
	r.err = errFormat
	return ""
}

// text reads an Unreal FText holding a plain string: flags, history type -1, has-string, string.
func (r *reader) text() string {
	r.i32() // flags
	if r.err != nil || r.i >= len(r.b) || int8(r.b[r.i]) != -1 {
		r.err = errFormat
		return ""
	}
	r.i++
	if r.i32() == 0 {
		return ""
	}
	return r.str()
}

func (r *reader) list() []string {
	n := int(r.i32())
	if n < 0 || n > 64 {
		r.err = errFormat
		return nil
	}
	out := make([]string, 0, n)
	for k := 0; k < n && r.err == nil; k++ {
		out = append(out, r.str())
	}
	return out
}

// ticks are 100 ns steps since 1 January of year 1 (.NET / Unreal FDateTime).
func fromTicks(t int64) time.Time {
	const unixEpochTicks = 621355968000000000
	if t < unixEpochTicks {
		return time.Time{}
	}
	return time.Unix(0, (t-unixEpochTicks)*100).UTC()
}

func (r *reader) record() Record {
	start := r.i
	var rec Record
	rec.Name = r.text()
	r.i32()
	rec.Saved = fromTicks(r.i64())
	rec.Version, rec.Car, rec.Track = r.str(), r.str(), r.str()
	rec.Surfaces, rec.Weathers, rec.Tags = r.list(), r.list(), r.list()
	n := int(r.i32())
	if n < 0 || n > 1000 {
		r.err = errFormat
	}
	for k := 0; k < n && r.err == nil; k++ {
		key := r.str()
		rec.Values = append(rec.Values, [2]string{key, r.str()})
	}
	if r.err == nil {
		rec.Raw = append([]byte(nil), r.b[start:r.i]...)
	}
	return rec
}

// ReadSave reads all setups in a save file.
func ReadSave(b []byte) ([]Record, error) {
	if !bytes.HasPrefix(b, []byte("GVAS")) {
		return nil, errFormat
	}
	at := bytes.Index(b, []byte("CarSetupsSaveGameData\x00"))
	if at < 4 {
		return nil, errFormat
	}
	r := &reader{b: b, i: at - 4}
	if r.str() != "CarSetupsSaveGameData" {
		return nil, errFormat
	}
	r.i64() // size of what follows
	cars := int(r.i32())
	if cars < 0 || cars > 10000 {
		return nil, errFormat
	}
	var out []Record
	for c := 0; c < cars && r.err == nil; c++ {
		r.str() // car id (each record repeats it)
		n := int(r.i32())
		if n < 0 || n > 100000 {
			return nil, errFormat
		}
		for s := 0; s < n && r.err == nil; s++ {
			out = append(out, r.record())
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	return out, nil
}

// ReadRecord reads one record on its own (a setup's stored content).
func ReadRecord(b []byte) (Record, error) {
	r := &reader{b: b}
	rec := r.record()
	if r.err != nil || r.i != len(b) {
		return Record{}, errFormat
	}
	return rec, nil
}
