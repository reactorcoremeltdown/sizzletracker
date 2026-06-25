package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Application state lives in the per-user config directory that each OS expects
// (os.UserConfigDir): ~/.config/sizzletracker on Linux/BSD,
// ~/Library/Application Support/sizzletracker on macOS, %AppData%\sizzletracker
// on Windows. It holds:
//
//	config.json   - persistent preferences (MIDI ports, pane split, last file)
//	recovery.sng  - autosaved copy of the working song for crash recovery

const appName = "sizzletracker"

// Config is the persisted preference set.
type Config struct {
	LowerH   int              `json:"lower_h,omitempty"`
	LastPath string           `json:"last_path,omitempty"`
	SaveDir  string           `json:"save_dir,omitempty"` // default folder for new projects
	NoThru   bool             `json:"no_thru,omitempty"`  // MIDI note thru disabled (default: on)
	Patch    []string         `json:"patch,omitempty"`    // "input>>output" routes
	Filters  map[string][]int `json:"filters,omitempty"`  // output -> passing channels
}

// appDir returns (and creates) the application's config directory.
func appDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func configPath() string {
	dir, err := appDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// recoveryPath is the autosave / crash-recovery project file.
func recoveryPath() string {
	dir, err := appDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "recovery.sng")
}

// loadConfig reads config.json (returns zero Config if absent/unreadable).
func loadConfig() Config {
	var c Config
	p := configPath()
	if p == "" {
		return c
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

func (c Config) save() error {
	p := configPath()
	if p == "" {
		return nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
