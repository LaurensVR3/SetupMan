package acc

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"

	"setupman/internal/game"
)

// sharedView keeps a shared-memory block mapped while the extra log runs (D-162), so a reading is
// one memory copy instead of opening the block every time.
type sharedView struct {
	handle windows.Handle
	addr   uintptr
	buf    []byte
}

func openView(name string, size int) (*sharedView, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, _, callErr := procOpenFileMappingW.Call(windows.FILE_MAP_READ, 0, uintptr(unsafe.Pointer(n)))
	if h == 0 {
		return nil, callErr
	}
	addr, err := windows.MapViewOfFile(windows.Handle(h), windows.FILE_MAP_READ, 0, 0, uintptr(size))
	if err != nil {
		windows.CloseHandle(windows.Handle(h))
		return nil, err
	}
	return &sharedView{handle: windows.Handle(h), addr: addr, buf: make([]byte, size)}, nil
}

func (v *sharedView) read() []byte {
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&v.buf[0])), v.addr, uintptr(len(v.buf)))
	return v.buf
}

func (v *sharedView) close() {
	windows.UnmapViewOfFile(v.addr)
	windows.CloseHandle(v.handle)
}

type extraReader struct {
	phys, graph *sharedView
	exes        []string
	reads       int
}

// OpenExtra maps ACC's physics and graphics pages for the extra log.
func (g *Game) OpenExtra() (game.ExtraReader, error) {
	if !processRunning(g.live.exes...) {
		return nil, errors.New("ACC is not running")
	}
	p, err := openView(physicsMapName, extraPhysicsSize)
	if err != nil {
		return nil, err
	}
	gr, err := openView(graphicsMapName, extraGraphicsSize)
	if err != nil {
		p.close()
		return nil, err
	}
	return &extraReader{phys: p, graph: gr, exes: g.live.exes}, nil
}

func (r *extraReader) Read(dst []float32) (live, ok bool) {
	// The mapping stays valid after ACC closes; check the program now and then (cheap, shared list).
	if r.reads++; r.reads%50 == 0 && !processRunning(r.exes...) {
		return false, false
	}
	return parseExtra(r.phys.read(), r.graph.read(), dst), true
}

func (r *extraReader) Close() {
	r.phys.close()
	r.graph.close()
}
