// Package watch reports changes inside a folder (including subfolders) without
// polling: Windows wakes us up only when something changes (spec D-13, D-32).
package watch

import (
	"golang.org/x/sys/windows"
)

const changeMask = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE

// Watch calls onChange (from a background goroutine) whenever something changes under dir.
// The returned stop function ends watching. Opening the folder never modifies it.
func Watch(dir string, onChange func()) (stop func(), err error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}
	changed, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(h)
		windows.CloseHandle(changed)
		return nil, err
	}
	done := make(chan struct{})

	// Register the first watch before returning, so no change right after Watch is missed.
	buf := make([]byte, 64*1024)
	ov := new(windows.Overlapped)
	ov.HEvent = changed
	if err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, changeMask, nil, ov, 0); err != nil {
		windows.CloseHandle(h)
		windows.CloseHandle(changed)
		windows.CloseHandle(quit)
		return nil, err
	}

	go func() {
		defer close(done)
		defer windows.CloseHandle(h)
		defer windows.CloseHandle(changed)
		first := true
		for {
			if !first {
				*ov = windows.Overlapped{HEvent: changed}
				if err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, changeMask, nil, ov, 0); err != nil {
					return // folder gone or not watchable
				}
			}
			first = false
			ev, err := windows.WaitForMultipleObjects([]windows.Handle{changed, quit}, false, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 {
				windows.CancelIoEx(h, ov)
				var n uint32
				windows.GetOverlappedResult(h, ov, &n, true) // wait until the cancel is done
				return
			}
			var n uint32
			if err := windows.GetOverlappedResult(h, ov, &n, false); err != nil && err != windows.ERROR_NOTIFY_ENUM_DIR {
				return
			}
			windows.ResetEvent(changed)
			onChange() // we don't need details: any change means "rescan this game"
		}
	}()

	return func() {
		windows.SetEvent(quit)
		<-done
		windows.CloseHandle(quit)
	}, nil
}
