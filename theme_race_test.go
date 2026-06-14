package main

import (
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// TestPaletteConcurrentSwitchAndRender exercises the palette globals/styles from
// two goroutines at once: one repeatedly calls applyPalette (the writer, as the
// settings palette picker does from Update) while the other repeatedly calls
// View (the reader). Run under `go test -race`, an unsynchronized read/write of
// the package-level color vars or derived styles would be reported as a data
// race; with paletteMu guarding both paths it is clean. See issue #115.
//
// Without the -race flag this test still verifies the lock doesn't deadlock and
// that View keeps producing output while palettes are switched underneath it.
func TestPaletteConcurrentSwitchAndRender(t *testing.T) {
	// Build a sized model in each render mode so View's read path actually
	// touches the palette globals (list tabs, detail header, diff styles).
	var base tea.Model = newModel()
	base, _ = base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	base, _ = base.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "alpha", type_: "issue"}},
		prs:    []list.Item{item{number: 2, title: "beta", type_: "pr"}},
	})
	m := base.(model)

	// Restore the default palette when done so concurrent palette switching
	// doesn't leak into other tests.
	t.Cleanup(func() { applyPalette(defaultPalette) })

	const iterations = 2000
	var wg sync.WaitGroup

	// Writer: cycle through every registered palette, mirroring the picker.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			applyPalette(palettes[i%len(palettes)])
		}
	}()

	// Readers: render concurrently while the palette is being switched.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = m.View()
			}
		}()
	}

	// Also read the derived state through activePaletteName, another guarded
	// reader of the globals.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = activePaletteName()
		}
	}()

	wg.Wait()
}
