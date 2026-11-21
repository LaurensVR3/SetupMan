//go:build windows

package autostart

import (
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The test uses its own registry key, so the real "Start with Windows" setting is never touched.
func TestSetEnabled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	KeyPath = `Software\SetupManTest\Run`
	defer func() {
		registry.DeleteKey(registry.CURRENT_USER, `Software\SetupManTest\Run`)
		registry.DeleteKey(registry.CURRENT_USER, `Software\SetupManTest`)
	}()

	if Enabled() {
		t.Fatal("should start disabled")
	}
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("not enabled after Set(true)")
	}
	cmd, _ := Command()
	if !strings.HasSuffix(cmd, `" --no-browser`) || !strings.HasPrefix(cmd, `"`) {
		t.Errorf("command = %q", cmd)
	}
	if err := Refresh(); err != nil {
		t.Error(err)
	}
	if err := Set(false); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Error("still enabled after Set(false)")
	}
	if err := Set(false); err != nil {
		t.Errorf("disabling twice should be fine: %v", err)
	}
}
