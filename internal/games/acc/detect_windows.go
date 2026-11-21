package acc

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// steamLibraries finds Steam via the registry and returns its library folders.
func steamLibraries() []string {
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

// documentsDir returns the real Documents folder (follows OneDrive redirection).
func documentsDir() string {
	p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if err != nil {
		return ""
	}
	return p
}
