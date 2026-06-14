package main

import (
	"os"
	"path/filepath"
	"testing"
)

// tempConfigDir points os.UserConfigDir at a per-test temp dir by setting both
// XDG_CONFIG_HOME (Linux) and HOME (macOS Application Support) so the real user
// config is never touched.
func tempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

// Saving then loading returns the same palette name.
func TestConfigRoundTrip(t *testing.T) {
	tempConfigDir(t)

	if err := saveConfig(config{Palette: "Dracula"}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	c, ok := loadConfig()
	if !ok {
		t.Fatal("loadConfig: expected a usable config")
	}
	if c.Palette != "Dracula" {
		t.Errorf("loaded palette = %q, want Dracula", c.Palette)
	}
	if p := loadPalette(); p.Name != "Dracula" {
		t.Errorf("loadPalette() = %q, want Dracula", p.Name)
	}
}

// A missing config file falls back to the default palette without error.
func TestLoadPaletteMissingFallsBack(t *testing.T) {
	tempConfigDir(t)
	if _, ok := loadConfig(); ok {
		t.Error("loadConfig should report not-ok for a missing file")
	}
	if p := loadPalette(); p.Name != defaultPalette.Name {
		t.Errorf("loadPalette() = %q, want default %q", p.Name, defaultPalette.Name)
	}
}

// Garbage / unknown content falls back to the default palette without crashing.
func TestLoadPaletteGarbageFallsBack(t *testing.T) {
	tempConfigDir(t)

	// Malformed JSON.
	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if p := loadPalette(); p.Name != defaultPalette.Name {
		t.Errorf("malformed: loadPalette() = %q, want default", p.Name)
	}

	// Valid JSON, unknown palette name.
	if err := saveConfig(config{Palette: "Nonexistent"}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	if p := loadPalette(); p.Name != defaultPalette.Name {
		t.Errorf("unknown name: loadPalette() = %q, want default", p.Name)
	}
}
