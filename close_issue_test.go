package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// closeCall records the arguments ghCloseIssue was invoked with.
type closeCall struct {
	number int
	reason string
	called bool
}

// withStubClose swaps the package-level ghCloseIssue with a stub for the duration
// of a test so the close dialog never shells out to a real `gh issue close`. It
// returns a pointer to the recorded call so assertions can inspect the args. err
// is returned by the stub so the error path can be exercised.
func withStubClose(t *testing.T, err error) *closeCall {
	t.Helper()
	rec := &closeCall{}
	orig := ghCloseIssue
	ghCloseIssue = func(number int, reason string) error {
		rec.called = true
		rec.number = number
		rec.reason = reason
		return err
	}
	t.Cleanup(func() { ghCloseIssue = orig })
	return rec
}

// Pressing `c` on an issue in list mode opens the close dialog over that issue.
func TestCloseDialogOpensFromList(t *testing.T) {
	tm := listModel(t) // issue #1 selected on the Issues tab

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})

	mm := tm.(model)
	if !mm.showCloseDialog {
		t.Fatal("c did not open the close dialog")
	}
	if mm.closeDialogItem == nil || mm.closeDialogItem.number != 1 {
		t.Fatalf("dialog captured wrong item: %+v", mm.closeDialogItem)
	}
	view := mm.View()
	for _, want := range []string{"Close issue", "Completed", "Not planned", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("close dialog missing %q", want)
		}
	}
}

// Pressing `c` in the detail view of an issue opens the close dialog.
func TestCloseDialogOpensFromDetail(t *testing.T) {
	m := openDetailWithBody(t, "body", 80, 24) // issue #1 detail open
	var tm tea.Model = m

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})

	mm := tm.(model)
	if !mm.showCloseDialog {
		t.Fatal("c did not open the close dialog from detail")
	}
	if mm.closeDialogItem == nil || mm.closeDialogItem.number != 1 {
		t.Fatalf("dialog captured wrong item: %+v", mm.closeDialogItem)
	}
}

// Selecting "Completed" closes the issue with the completed state reason.
func TestCloseDialogCompleted(t *testing.T) {
	rec := withStubClose(t, nil)
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyEnter}) // cursor on Completed
	if cmd == nil {
		t.Fatal("selecting Completed produced no command")
	}
	cmd()

	if !rec.called {
		t.Fatal("Completed did not invoke ghCloseIssue")
	}
	if rec.number != 1 || rec.reason != closeReasonCompleted {
		t.Errorf("got (%d, %q), want (1, %q)", rec.number, rec.reason, closeReasonCompleted)
	}
}

// Moving the cursor to "Not planned" and selecting it uses the not-planned reason.
func TestCloseDialogNotPlanned(t *testing.T) {
	rec := withStubClose(t, nil)
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown}) // -> Not planned
	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting Not planned produced no command")
	}
	cmd()

	if rec.reason != closeReasonNotPlanned {
		t.Errorf("reason = %q, want %q", rec.reason, closeReasonNotPlanned)
	}
}

// Selecting "Cancel" dismisses the dialog without calling the API.
func TestCloseDialogCancelChoice(t *testing.T) {
	rec := withStubClose(t, nil)
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown}) // Not planned
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown}) // Cancel
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}

	mm := tm.(model)
	if mm.showCloseDialog {
		t.Error("Cancel should close the dialog")
	}
	if rec.called {
		t.Error("Cancel should not invoke ghCloseIssue")
	}
}

// Esc dismisses the dialog with no change and no API call.
func TestCloseDialogEscDismisses(t *testing.T) {
	rec := withStubClose(t, nil)
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})

	mm := tm.(model)
	if mm.showCloseDialog {
		t.Error("esc should dismiss the close dialog")
	}
	if rec.called {
		t.Error("esc should not invoke ghCloseIssue")
	}
}

// A successful close marks the issue closed so its list row reflects the state.
func TestCloseIssueReflectsState(t *testing.T) {
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd() // closeIssueResultMsg{number:1}
	tm, _ = tm.Update(msg)

	mm := tm.(model)
	it, ok := mm.issueList.Items()[0].(item)
	if !ok {
		t.Fatal("first list item is not an item")
	}
	if !it.closed {
		t.Error("issue should be marked closed after a successful close")
	}
	if !strings.Contains(it.Title(), "[closed]") {
		t.Errorf("closed row title missing marker: %q", it.Title())
	}
}

// A failed close surfaces the error on the model without crashing and leaves the
// issue unmarked.
func TestCloseIssueErrorSurfaced(t *testing.T) {
	withStubClose(t, errors.New("boom"))
	tm := listModel(t)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	_, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	tm, _ = tm.Update(msg)

	mm := tm.(model)
	if mm.err == nil {
		t.Fatal("a failed close should surface an error")
	}
	it := mm.issueList.Items()[0].(item)
	if it.closed {
		t.Error("a failed close should not mark the issue closed")
	}
}

// `c` on the PRs tab is a no-op: PRs can't be closed via this dialog.
func TestCloseDialogNotOpenedForPR(t *testing.T) {
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab}) // PRs tab (#2)

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})

	if tm.(model).showCloseDialog {
		t.Error("c on a PR must not open the close dialog")
	}
}

// `c` while filtering is a literal filter character and must not open the dialog.
func TestCloseDialogNotOpenedWhileFiltering(t *testing.T) {
	tm := listModel(t)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !tm.(model).currentList().SettingFilter() {
		t.Fatal("setup: not in filter mode")
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if tm.(model).showCloseDialog {
		t.Error("c while filtering must not open the close dialog")
	}
}

// The dialog swallows unrelated keys so they don't act on the obscured view.
func TestCloseDialogSwallowsKeys(t *testing.T) {
	tm := listModel(t)
	before := tm.(model).activeTab

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})

	mm := tm.(model)
	if !mm.showCloseDialog {
		t.Error("dialog should stay open after an unrelated key")
	}
	if mm.activeTab != before {
		t.Error("tab should not switch while the dialog is open")
	}
}

// `c` on an empty list is a safe no-op (no panic, dialog stays closed).
func TestCloseDialogEmptyListNoOp(t *testing.T) {
	m := newModel()
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{}, prs: []list.Item{}})

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})

	if tm.(model).showCloseDialog {
		t.Error("c on an empty list should not open the dialog")
	}
}
