package ac

// Reading a car's data.acd (spec D-141): Assetto Corsa keeps each car's data files (setup.ini and
// others) in one lightly encrypted archive. The format and the key, which is made from the car's
// folder name, follow the open-source Assetto Server Manager (MIT licence,
// github.com/JustaPenguin/assetto-server-manager, pkg/acd), which credits AcTools / Content Manager
// and Luigi Auriemma's QuickBMS script. Read only: SetupMan never changes a game file.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// acdFile returns one file (e.g. "setup.ini") from the bytes of a data.acd; folder is the car's
// folder name, which the key is made from.
func acdFile(data []byte, folder, name string) ([]byte, error) {
	key := acdKey(folder)
	pos := 0
	read32 := func() (int32, bool) {
		if pos+4 > len(data) {
			return 0, false
		}
		v := int32(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
		return v, true
	}
	for pos < len(data) {
		n, ok := read32()
		if !ok {
			break
		}
		if n == -1111 { // a marker: skip one number, then the name length follows
			if _, ok = read32(); !ok {
				break
			}
			if n, ok = read32(); !ok {
				break
			}
		}
		if n < 0 || pos+int(n) > len(data) {
			return nil, errors.New("data.acd: damaged")
		}
		fname := string(data[pos : pos+int(n)])
		pos += int(n)
		count, ok := read32()
		if !ok || count < 0 || pos+int(count)*4 > len(data) {
			return nil, errors.New("data.acd: damaged")
		}
		body := data[pos : pos+int(count)*4]
		pos += int(count) * 4
		if strings.EqualFold(fname, name) {
			return acdDecode(body, key), nil
		}
	}
	return nil, fmt.Errorf("data.acd has no %s", name)
}

// acdDecode removes the key from a file's bytes; each character is stored as 4 bytes (UTF-32).
func acdDecode(body []byte, key string) []byte {
	buf := append([]byte(nil), body...)
	x := 0
	for i := range buf {
		if buf[i] == 0 {
			continue
		}
		buf[i] -= key[x%len(key)]
		x++
	}
	var b strings.Builder
	for i := 0; i+3 < len(buf); i += 4 {
		b.WriteRune(rune(binary.LittleEndian.Uint32(buf[i:])))
	}
	return []byte(b.String())
}

// acdKey makes the archive key from the car's folder name.
func acdKey(s string) string {
	part1 := 0
	for i := 0; i < len(s); i++ {
		part1 += int(s[i])
	}
	part2 := 0
	for i := 0; i < len(s)-1; i++ {
		part2 *= int(s[i])
		i++
		part2 -= int(s[i])
	}
	part3 := 0
	for i := 1; i < len(s)-3; i += 4 {
		part3 *= int(s[i])
		i++
		part3 /= int(s[i]) + 0x1b
		i -= 2
		part3 += -0x1b - int(s[i])
	}
	part4 := 0x1683
	for i := 1; i < len(s); i++ {
		part4 -= int(s[i])
	}
	part5 := 0x42
	for i := 1; i < len(s)-4; i += 4 {
		n := (int(s[i]) + 0xf) * part5
		i--
		x := int(s[i])
		i++
		x += 0xf
		x *= n
		x += 0x16
		part5 = x
	}
	part6 := 0x65
	for i := 0; i < len(s)-2; i += 2 {
		part6 -= int(s[i])
	}
	part7 := 0xab
	for i := 0; i < len(s)-2; i += 2 {
		part7 %= int(s[i])
	}
	part8 := 0xab
	for i := 0; i < len(s)-1; i++ {
		part8 /= int(s[i])
		i++
		part8 += int(s[i])
		i--
	}
	return fmt.Sprintf("%d-%d-%d-%d-%d-%d-%d-%d",
		part1&0xff, part2&0xff, part3&0xff, part4&0xff, part5&0xff, part6&0xff, part7&0xff, part8&0xff)
}
