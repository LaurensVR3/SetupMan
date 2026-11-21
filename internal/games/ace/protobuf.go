package ace

import (
	"encoding/binary"
	"errors"
	"math"
	"strconv"
)

// AC EVO saves setups in protobuf format: numbered fields without names. This reads any such
// file into paths like "4.1" (field 1 inside field 4). Repeated fields get an index:
// "4[2].1" is field 1 of the third field 4.

type pbValue struct {
	path  string
	num   float64
	text  string // for strings and packed floats
	isNum bool
}

var errFormat = errors.New("not an AC EVO setup file")

func uvarint(b []byte) (uint64, int) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0
	}
	return v, n
}

// decode reads one message. prefix is the path so far ("" at the top).
func decode(b []byte, prefix string, depth int, out *[]pbValue) error {
	if depth > 8 {
		return errFormat
	}
	count := map[uint64]int{}
	type raw struct {
		field uint64
		wire  uint64
		data  []byte
		num   uint64
	}
	var items []raw
	for len(b) > 0 {
		tag, n := uvarint(b)
		if n == 0 {
			return errFormat
		}
		b = b[n:]
		r := raw{field: tag >> 3, wire: tag & 7}
		if r.field == 0 {
			return errFormat
		}
		switch r.wire {
		case 0:
			v, n := uvarint(b)
			if n == 0 {
				return errFormat
			}
			r.num, b = v, b[n:]
		case 1:
			if len(b) < 8 {
				return errFormat
			}
			r.data, b = b[:8], b[8:]
		case 5:
			if len(b) < 4 {
				return errFormat
			}
			r.data, b = b[:4], b[4:]
		case 2:
			l, n := uvarint(b)
			if n == 0 || uint64(len(b)-n) < l {
				return errFormat
			}
			r.data, b = b[n:n+int(l)], b[n+int(l):]
		default:
			return errFormat
		}
		count[r.field]++
		items = append(items, r)
	}
	seen := map[uint64]int{}
	for _, r := range items {
		p := prefix + strconv.FormatUint(r.field, 10)
		if count[r.field] > 1 {
			p += "[" + strconv.Itoa(seen[r.field]) + "]"
		}
		seen[r.field]++
		switch r.wire {
		case 0:
			*out = append(*out, pbValue{path: p, num: float64(r.num), isNum: true})
		case 1:
			*out = append(*out, pbValue{path: p, num: math.Float64frombits(binary.LittleEndian.Uint64(r.data)), isNum: true})
		case 5:
			*out = append(*out, pbValue{path: p, num: float64(math.Float32frombits(binary.LittleEndian.Uint32(r.data))), isNum: true})
		case 2:
			if printable(r.data) {
				*out = append(*out, pbValue{path: p, text: string(r.data)})
				continue
			}
			var sub []pbValue
			if err := decode(r.data, p+".", depth+1, &sub); err == nil {
				*out = append(*out, sub...)
				continue
			}
			if len(r.data)%4 == 0 { // packed floats, e.g. both anti-roll bars in one field
				for i := 0; i+4 <= len(r.data); i += 4 {
					f := math.Float32frombits(binary.LittleEndian.Uint32(r.data[i:]))
					*out = append(*out, pbValue{path: p + "[" + strconv.Itoa(i/4) + "]", num: float64(f), isNum: true})
				}
				continue
			}
			return errFormat
		}
	}
	return nil
}

func printable(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	for _, c := range b {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}
