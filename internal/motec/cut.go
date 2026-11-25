package motec

// Cutting laps out of a log for a data pack (spec D-201). The source file is only read; the new,
// small log is written to a writer (never next to ACC's files, D-160).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const (
	cutBefore = 1.0 // seconds of the lap clock kept before the first lap, so its start is seen
	cutAfter  = 0.5 // and after the last lap (under 1 s, so no extra part lap appears after it)
)

// CutResult says where the cut part sits in the source and which laps it holds.
type CutResult struct {
	// Offset is where the new log starts, in seconds from the start of the source log:
	// a time t in the source is t - Offset in the new log.
	Offset float64
	// Crossings are the line crossings in the new log, in seconds (for the .ldx file).
	Crossings []float64
	Channels  int // channels written (a channel without data for these laps is left out)
}

// Cut writes a new .ld file holding only the laps from first to last (lap numbers as Laps gives
// them; both must be complete laps). The new log has the source's header and channel descriptions
// and exactly the source's samples, so it reads back with the same lap times to the millisecond and
// opens in MoTeC i2.
//
// It is laid out like ACC's own files: the lap clock (TIME) runs a little longer than the laps, and
// every other channel either covers that same stretch or holds exactly the laps (ending at the last
// line crossing), which is how Laps works out when each channel starts.
func (lf *File) Cut(w io.Writer, first, last int) (CutResult, error) {
	laps, err := lf.Laps()
	if err != nil {
		return CutResult{}, err
	}
	if first < 0 || last < first || last >= len(laps) {
		return CutResult{}, errors.New("motec: no such lap")
	}
	for _, l := range laps[first : last+1] {
		if !l.Complete {
			return CutResult{}, fmt.Errorf("motec: lap %d is not a full lap", l.N)
		}
	}
	startS, endS := laps[first].Start, laps[last].Start+laps[last].Time

	clock, ok := lf.Channel("TIME")
	if !ok {
		return CutResult{}, errors.New("motec: the log has no lap clock")
	}
	hz := float64(clock.Hz)
	tFrom := max(0, int(math.Floor((startS-cutBefore)*hz)))
	tTo := min(clock.Count, int(math.Ceil((endS+cutAfter)*hz)))
	res := CutResult{Offset: float64(tFrom) / hz}
	full := float64(tTo-tFrom) / hz

	type part struct {
		c        Channel
		from, to int
	}
	var parts []part
	for _, c := range lf.Channels {
		if strings.EqualFold(c.Name, "TIME") {
			parts = append(parts, part{c, tFrom, tTo})
			continue
		}
		ch := float64(c.Hz)
		// the same stretch as the clock, when the channel has it
		from := int(math.Round((res.Offset - c.Start) * ch))
		to := from + int(math.Round(full*ch))
		if from >= 0 && to <= c.Count && to > from {
			parts = append(parts, part{c, from, to})
			continue
		}
		// else exactly the laps
		from = int(math.Round((startS - c.Start) * ch))
		to = int(math.Round((endS - c.Start) * ch))
		if from >= 0 && to <= c.Count && to > from {
			parts = append(parts, part{c, from, to})
		}
	}

	// the source's header, event and venue blocks: everything before the first channel description
	le := binary.LittleEndian
	hdr := make([]byte, headerSize)
	if _, err := lf.f.ReadAt(hdr, 0); err != nil {
		return CutResult{}, err
	}
	prefix := int64(min(le.Uint32(hdr[8:]), le.Uint32(hdr[12:])))
	if prefix < headerSize || prefix > lf.size {
		return CutResult{}, ErrFormat
	}
	head := make([]byte, prefix)
	if _, err := lf.f.ReadAt(head, 0); err != nil {
		return CutResult{}, err
	}
	metaAt := prefix
	dataAt := metaAt + int64(len(parts))*channelSize
	le.PutUint32(head[8:], uint32(metaAt))
	le.PutUint32(head[12:], uint32(dataAt))
	le.PutUint16(head[0x56:], uint16(len(parts))) // number of channels

	metas := make([]byte, len(parts)*channelSize)
	off := dataAt
	for i, p := range parts {
		m := metas[i*channelSize : (i+1)*channelSize]
		if _, err := lf.f.ReadAt(m, p.c.meta); err != nil {
			return CutResult{}, err
		}
		prev, next := uint32(0), uint32(0)
		if i > 0 {
			prev = uint32(metaAt + int64(i-1)*channelSize)
		}
		if i < len(parts)-1 {
			next = uint32(metaAt + int64(i+1)*channelSize)
		}
		le.PutUint32(m[0:], prev)
		le.PutUint32(m[4:], next)
		le.PutUint32(m[8:], uint32(off))
		le.PutUint32(m[12:], uint32(p.to-p.from))
		off += int64(p.to-p.from) * int64(p.c.size)
	}
	if off > math.MaxUint32 {
		return CutResult{}, errors.New("motec: the cut part is too large")
	}
	if _, err := w.Write(head); err != nil {
		return CutResult{}, err
	}
	if _, err := w.Write(metas); err != nil {
		return CutResult{}, err
	}
	for _, p := range parts { // the stored bytes as they are: no rounding of any value
		n := int64(p.to-p.from) * int64(p.c.size)
		if _, err := io.Copy(w, io.NewSectionReader(lf.f, p.c.data+int64(p.from)*int64(p.c.size), n)); err != nil {
			return CutResult{}, err
		}
	}
	res.Channels = len(parts)
	res.Crossings = append(res.Crossings, startS-res.Offset)
	for _, l := range laps[first : last+1] {
		res.Crossings = append(res.Crossings, l.Start+l.Time-res.Offset)
	}
	return res, nil
}

// WriteLDX writes the .ldx file that goes with a log: the line crossings (seconds from the start
// of the log) as MoTeC i2's lap markers, in the form ACC writes them.
func WriteLDX(w io.Writer, crossings []float64) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><LDXFile Locale="English_Australia.1252" DefaultLocale="C" Version="1.6"><Layers><Layer LayerName=""><MarkerBlock><MarkerGroup Name="Beacons">`)
	for i, c := range crossings {
		fmt.Fprintf(&b, `<Marker Version="100" ClassName="BCN" Name="%d, id=99" Flags="13" Time="%d" />`, i, int64(math.Round(c*1e6)))
	}
	b.WriteString(`</MarkerGroup></MarkerBlock></Layer></Layers></LDXFile>`)
	_, err := io.WriteString(w, b.String())
	return err
}
