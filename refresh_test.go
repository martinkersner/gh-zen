package main

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func mkItems(n int, type_ string) []list.Item {
	items := make([]list.Item, n)
	for i := 0; i < n; i++ {
		items[i] = item{number: i + 1, title: "item", type_: type_}
	}
	return items
}

func TestRestoreIndex(t *testing.T) {
	cases := []struct {
		name  string
		count int
		set   int
		want  int
	}{
		{"middle preserved", 5, 3, 3},
		{"clamped to last when shrunk", 2, 4, 1},
		{"empty stays zero", 0, 3, 0},
		{"negative coerced to zero", 5, -2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := list.New(mkItems(c.count, "issue"), newItemDelegate(), 80, 24)
			restoreIndex(&l, c.set)
			if got := l.Index(); got != c.want {
				t.Errorf("restoreIndex(count=%d, set=%d): got %d, want %d", c.count, c.set, got, c.want)
			}
		})
	}
}

// A refresh (new dataMsg) must keep the list cursor where it was instead of
// snapping back to the top.
func TestRefreshPreservesSelection(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue"), prs: mkItems(3, "pr")})

	// Move the cursor down a few rows.
	for i := 0; i < 4; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if got := tm.(model).issueList.Index(); got != 4 {
		t.Fatalf("setup: expected index 4, got %d", got)
	}

	// A refresh delivers a fresh dataMsg; selection should be retained.
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue"), prs: mkItems(3, "pr")})
	if got := tm.(model).issueList.Index(); got != 4 {
		t.Errorf("after refresh: index = %d, want 4 (selection not preserved)", got)
	}
}

// A refresh that returns fewer items clamps the selection to the last item.
func TestRefreshClampsSelectionWhenListShrinks(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(10, "issue")})
	for i := 0; i < 8; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if got := tm.(model).issueList.Index(); got != 8 {
		t.Fatalf("setup: expected index 8, got %d", got)
	}

	tm, _ = tm.Update(dataMsg{issues: mkItems(3, "issue")})
	if got := tm.(model).issueList.Index(); got != 2 {
		t.Errorf("after shrink: index = %d, want 2 (last item)", got)
	}
}

// A tick re-arms the ticker (returns a command) and does not change which view
// is showing.
func TestTickReArmsTicker(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(2, "issue")})

	tm, cmd := tm.Update(tickMsg{})
	if cmd == nil {
		t.Error("tick returned nil cmd; ticker not re-armed")
	}
	if tm.(model).detailOpen {
		t.Error("tick should not open the detail view")
	}
}

// While the filter input is active, a tick still re-arms the ticker but must not
// reshuffle the list under the user.
func TestTickSkipsRefreshWhileFiltering(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(5, "issue")})

	// Enter filter mode.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !tm.(model).issueList.SettingFilter() {
		t.Fatal("setup: expected to be in filter input")
	}

	tm, cmd := tm.Update(tickMsg{})
	if cmd == nil {
		t.Error("tick returned nil cmd while filtering; ticker not re-armed")
	}
}

// Pressing r in the list view dispatches a refresh command.
func TestRKeyTriggersListRefresh(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: mkItems(2, "issue")})

	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Error("r in list view returned nil cmd; no refresh dispatched")
	}
}

// A failed body refresh keeps the previously cached body instead of blanking it.
func TestBodyRefreshErrorKeepsCachedBody(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{
		item{number: 1, title: "x", body: "original body", type_: "issue"},
	}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !tm.(model).detailOpen {
		t.Fatal("setup: detail did not open")
	}
	key := cacheKey(tm.(model).detailItem)

	tm, _ = tm.Update(bodyMsg{key: key, err: errFake{}})
	mm := tm.(model)
	if mm.detailLoading {
		t.Error("detailLoading should be cleared after a failed refresh")
	}
	if mm.cachedBody(mm.detailItem) != "original body" {
		t.Errorf("cached body lost after failed refresh: %q", mm.cachedBody(mm.detailItem))
	}
}

type errFake struct{}

func (errFake) Error() string { return "fake fetch error" }
