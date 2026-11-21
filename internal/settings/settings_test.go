package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsWhenMissingOrDamaged(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "settings.json"))
	if got := s.Load(); got.Theme != "system" || got.Threshold != 95 || !got.DownloadLogos {
		t.Errorf("defaults wrong: %+v", got)
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{broken"), 0o644)
	if got := s.Load(); got.Threshold != 95 {
		t.Errorf("damaged file should give defaults: %+v", got)
	}
}

func TestSaveLoad(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sub", "settings.json"))
	st := Defaults()
	st.Theme = "dark"
	st.Threshold = 90.5
	st.DownloadLogos = false
	st.Games["acc"] = Game{Visibility: VisShow, Folder: `C:\x`}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got := s.Load()
	if got.Theme != "dark" || got.Threshold != 90.5 || got.DownloadLogos || got.Games["acc"].Folder != `C:\x` {
		t.Errorf("round trip: %+v", got)
	}
	bad := st
	bad.Theme = "pink"
	if s.Save(bad) == nil {
		t.Error("invalid theme accepted")
	}
	bad = st
	bad.Games = map[string]Game{"acc": {Visibility: "maybe"}}
	if s.Save(bad) == nil {
		t.Error("invalid visibility accepted")
	}
}

func TestMigratePhase1(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte(`{"folder":"C:\\Dev\\ACC Setups","threshold":93}`), 0o644)
	got := NewStore(p).Load()
	if got.Threshold != 93 || !got.DownloadLogos || got.Theme != "system" {
		t.Errorf("old values / defaults wrong: %+v", got)
	}
	if g := got.Games["acc"]; g.Visibility != VisShow || g.Folder != `C:\Dev\ACC Setups` {
		t.Errorf("folder not migrated: %+v", got.Games)
	}
}

func TestManagedFor(t *testing.T) {
	g := Game{Managed: true, ManagedFolder: `C:\Users\x\Setups`}
	if !g.ManagedFor(`c:\users\x\setups\`) {
		t.Error("same folder, other case")
	}
	if g.ManagedFor(`C:\Other`) || g.ManagedFor("") {
		t.Error("other folder counted as managed")
	}
	if (Game{Managed: true}).ManagedFor(`C:\Users\x\Setups`) {
		t.Error("no managed folder counted as managed")
	}
}
