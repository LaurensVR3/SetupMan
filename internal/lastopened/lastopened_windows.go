package lastopened

import (
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Open opens a file for reading and tells Windows not to update its last-opened time for this
// handle (SetFileTime with 0xFFFFFFFF, documented for exactly this purpose). The share mode lets the
// game read, write, rename and delete the file meanwhile. Asking for "write attributes" does not
// block the game: Windows ignores attribute access when checking sharing. If that access is refused,
// the file is opened normally (it then gets SetupMan's read time, which is only a lost clue).
func Open(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.FILE_WRITE_ATTRIBUTES, share, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return os.Open(path)
	}
	keep := windows.Filetime{LowDateTime: 0xFFFFFFFF, HighDateTime: 0xFFFFFFFF}
	_ = windows.SetFileTime(h, nil, &keep, nil)
	return os.NewFile(uintptr(h), path), nil
}

func accessTime(fi os.FileInfo) (time.Time, bool) {
	if fi == nil {
		return time.Time{}, false
	}
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok || d == nil {
		return time.Time{}, false
	}
	return time.Unix(0, d.LastAccessTime.Nanoseconds()), true
}

// enabledSetting reads NtfsDisableLastAccessUpdate (readable without administrator rights). Since
// Windows 10 1803: 0x80000000 user managed on, 0x80000001 user managed off, 0x80000002 system
// managed on, 0x80000003 system managed off; older Windows: 0 on, 1 off. The lowest bit = off.
func enabledSetting() (enabled, known bool) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\FileSystem`, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("NtfsDisableLastAccessUpdate")
	if err != nil {
		return false, false
	}
	return v&1 == 0, true
}
