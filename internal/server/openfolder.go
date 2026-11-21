package server

import (
	"os/exec"
	"runtime"
)

// openFolder shows a folder in the file manager of the PC SetupMan runs on.
func openFolder(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", dir).Start() // explorer reports exit code 1 even when it worked
	case "darwin":
		return exec.Command("open", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}
