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

	c := Config{MidiOut: "IAC Bus 1", MidiIn: "Keystation", LowerH: 12, LastPath: "/songs/x.sng"}
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := loadConfig(); got != c {
		t.Errorf("round-trip: got %+v, want %+v", got, c)
	}
}

func TestRealPortName(t *testing.T) {
	for in, want := range map[string]string{
		"<off>":         "",
		"<none>":        "",
		"<no portmidi>": "",
		"IAC Driver":    "IAC Driver",
	} {
		if got := realPortName(in); got != want {
			t.Errorf("realPortName(%q) = %q, want %q", in, got, want)
		}
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
