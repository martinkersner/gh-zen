package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config is the persisted user configuration. Only the selected palette name is
// stored; it is restored on next launch. Kept intentionally tiny.
type config struct {
	Palette string `json:"palette"`
}

// configPath returns the path to the config file under the user's config dir
// (os.UserConfigDir honors XDG_CONFIG_HOME on Linux and uses Application Support
// on macOS). Returns an error only if the base dir can't be determined.
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh-zen", "config.json"), nil
}

// loadConfig reads the persisted config. A missing, unreadable, or malformed
// file is not an error to the caller: it returns a zero config so startup falls
// back to the default palette. The bool reports whether a usable config was
// loaded.
func loadConfig() (config, bool) {
	path, err := configPath()
	if err != nil {
		return config{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, false
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return config{}, false
	}
	return c, true
}

// saveConfig persists c, creating the config dir if needed. Errors are returned
// so the caller can decide (the TUI ignores them so a failed write just means
// the choice isn't persisted).
func saveConfig(c config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// loadPalette resolves the persisted palette into a registered Palette. A
// missing config or an unknown name falls back to the default palette.
func loadPalette() Palette {
	c, ok := loadConfig()
	if !ok {
		return defaultPalette
	}
	if p, found := paletteByName(c.Palette); found {
		return p
	}
	return defaultPalette
}
