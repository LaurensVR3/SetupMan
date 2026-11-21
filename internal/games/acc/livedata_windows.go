package acc

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procOpenFileMappingW = kernel32.NewProc("OpenFileMappingW")
	procRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")
)

// readShared copies the first size bytes of a named shared memory block (read-only).
func readShared(name string, size int) ([]byte, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, _, callErr := procOpenFileMappingW.Call(windows.FILE_MAP_READ, 0, uintptr(unsafe.Pointer(n)))
	if h == 0 {
		return nil, callErr
	}
	handle := windows.Handle(h)
	defer windows.CloseHandle(handle)

	addr, err := windows.MapViewOfFile(handle, windows.FILE_MAP_READ, 0, 0, uintptr(size))
	if err != nil {
		return nil, err
	}
	defer windows.UnmapViewOfFile(addr)

	// Copy with a Windows call, so Go never has to turn the raw address into a pointer.
	out := make([]byte, size)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), addr, uintptr(size))
	return out, nil
}
