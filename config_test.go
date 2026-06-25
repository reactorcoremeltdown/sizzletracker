package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)                                      // macOS: ~/Library/...
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config")) // Linux: $XDG_CONFIG_HOME

	dir, err := appDir()
	if err != nil {
		t.Fatalf("appDir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("app dir not created: %v", err)
	}
	if !strings.Contains(dir, appName) {
		t.Errorf("app dir %q does not contain %q", dir, appName)
	}
	if !strings.HasPrefix(dir, tmp) {
		t.Errorf("app dir %q not under temp HOME %q", dir, tmp)
	}

	c := Config{
		LowerH:   12,
		LastPath: "/songs/x.sng",
		SaveDir:  "/songs",
		NoThru:   true,
		Patch:    []string{"Tracker>>IAC Bus 1", "Keystation>>IAC Bus 1"},
		Filters:  map[string][]int{"IAC Bus 1": {0, 1, 2}},
	}
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadConfig()
	if got.LowerH != c.LowerH || got.LastPath != c.LastPath || got.SaveDir != c.SaveDir || got.NoThru != c.NoThru {
		t.Errorf("scalars: got %+v, want %+v", got, c)
	}
	if len(got.Patch) != 2 || got.Patch[0] != "Tracker>>IAC Bus 1" {
		t.Errorf("patch round-trip: %v", got.Patch)
	}
	if chans := got.Filters["IAC Bus 1"]; len(chans) != 3 || chans[2] != 2 {
		t.Errorf("filters round-trip: %v", got.Filters)
	}
}

func TestLatchMode(t *testing.T) {
	e := newEditor()
	for _, c := range []struct {
		armed, thru bool
		want        string
	}{
		{false, true, "Playback"},
		{true, false, "Record"},
		{true, true, "Both"},
		{false, false, "Off"},
	} {
		e.armed, e.thru = c.armed, c.thru
		if got := e.latchMode(); got != c.want {
			t.Errorf("armed=%v thru=%v -> %q, want %q", c.armed, c.thru, got, c.want)
		}
	}
}

func TestSetNoteThru(t *testing.T) {
	m := fakeEngine([]string{"A"}, []string{"K"})
	if m.noThru {
		t.Errorf("default should forward notes (noThru=false)")
	}
	m.setNoteThru(false)
	if !m.noThru {
		t.Errorf("setNoteThru(false) should set noThru")
	}
	m.setNoteThru(true)
	if m.noThru {
		t.Errorf("setNoteThru(true) should clear noThru")
	}
}

func TestRecoveryRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))

	rp := recoveryPath()
	if rp == "" {
		t.Fatal("empty recovery path")
	}
	if fileExists(rp) {
		t.Fatal("recovery should not exist yet")
	}
	s := newSong()
	s.Blocks[0].Tracks[0].Steps[0] = Step{Note: 64, Vel: 100, Chan: 0}
	if err := writeFile(rp, []byte(encodeProject(s))); err != nil {
		t.Fatalf("write recovery: %v", err)
	}
	if !fileExists(rp) {
		t.Fatal("recovery file missing after write")
	}
	got, err := loadProject(rp)
	if err != nil {
		t.Fatalf("load recovery: %v", err)
	}
	if got.Blocks[0].Tracks[0].Steps[0].Note != 64 {
		t.Errorf("recovered note = %d, want 64", got.Blocks[0].Tracks[0].Steps[0].Note)
	}
}
