package gamekit

import (
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// SteamLibraries finds Steam via the registry and returns its library folders.
func SteamLibraries() []string {
	for _, k := range []struct {
		root registry.Key
		path string
		name string
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\Valve\Steam`, "InstallPath"},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := key.GetStringValue(k.name)
		key.Close()
		if err == nil && v != "" {
			return librariesFromSteam(v)
		}
	}
	return nil
}

func known(id *windows.KNOWNFOLDERID) string {
	p, err := windows.KnownFolderPath(id, 0)
	if err != nil {
		return ""
	}
	return p
}

// Documents is the real Documents folder (follows OneDrive redirection).
func Documents() string { return known(windows.FOLDERID_Documents) }

// SavedGames is the user's "Saved Games" folder.
func SavedGames() string { return known(windows.FOLDERID_SavedGames) }

// LocalAppData is %LOCALAPPDATA% (e.g. C:\Users\<name>\AppData\Local).
func LocalAppData() string { return known(windows.FOLDERID_LocalAppData) }

// Listing process IDs is cheap (~0.1 ms); a full process snapshot costs several ms. So names are
// kept per process ID, and only processes that are new since the last check are looked up. A
// snapshot is taken only when a new process can't be opened (protected programs) and once a
// minute, so a reused process ID never keeps an old name for long.
var procNames struct {
	byPID    map[uint32]string
	snapshot time.Time
}

const fullSnapshotEvery = time.Minute

// processNames lists the file names of all running programs (lower case).
func processNames() (map[string]bool, error) {
	ids := make([]uint32, 4096)
	var n uint32
	if err := windows.EnumProcesses(ids, &n); err != nil {
		return snapshotNames()
	}
	ids = ids[:n/4]
	if procNames.byPID == nil || time.Since(procNames.snapshot) >= fullSnapshotEvery {
		return snapshotNames()
	}
	next := make(map[uint32]string, len(ids))
	for _, id := range ids {
		name, ok := procNames.byPID[id]
		if !ok {
			if name, ok = imageName(id); !ok {
				return snapshotNames() // a new process we can't open: let the snapshot name it
			}
		}
		next[id] = name
	}
	procNames.byPID = next
	out := make(map[string]bool, len(next))
	for _, name := range next {
		if name != "" {
			out[name] = true
		}
	}
	return out, nil
}

// imageName is the program file name of one process (lower case). Process 0 and 4 (the system)
// have no file; they count as found with an empty name.
func imageName(pid uint32) (string, bool) {
	if pid == 0 || pid == 4 {
		return "", true
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", false
	}
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:size]))), true
}

// snapshotNames takes a full process snapshot and refreshes the names per process ID.
func snapshotNames() (map[string]bool, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	out := map[string]bool{}
	byPID := map[uint32]string{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		name := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		out[name] = true
		byPID[e.ProcessID] = name
	}
	procNames.byPID, procNames.snapshot = byPID, time.Now()
	return out, nil
}

// StartedAt returns when the earliest running program with one of these file names started
// (D-147: only setups the game opened since then can be the one being driven). It takes its own
// process snapshot, so call it only now and then (once per setup recognition).
func StartedAt(exes ...string) (time.Time, bool) {
	want := map[string]bool{}
	for _, x := range exes {
		want[strings.ToLower(x)] = true
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(snap)
	var first time.Time
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if !want[strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))] {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID)
		if err != nil {
			continue
		}
		var created, exited, kernel, user windows.Filetime
		if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) == nil {
			if t := time.Unix(0, created.Nanoseconds()); first.IsZero() || t.Before(first) {
				first = t
			}
		}
		windows.CloseHandle(h)
	}
	return first, !first.IsZero()
}
