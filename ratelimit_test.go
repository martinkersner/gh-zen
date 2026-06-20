package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// A reading with a zero resetAt (the field was absent, e.g. a test fake that
// doesn't populate rateLimit) must not flip the snapshot valid, so the backoff
// gate stays inert.
func TestSetRateLimitIgnoresZeroResetAt(t *testing.T) {
	c := &githubConn{}
	c.setRateLimit(rateLimitNode{Remaining: 5}) // ResetAt zero
	if c.rateLimitState().valid {
		t.Fatal("zero-resetAt reading should not be recorded as valid")
	}
}

// A real reading is stored and reported back.
func TestSetRateLimitStoresReading(t *testing.T) {
	c := &githubConn{}
	reset := time.Now().Add(time.Hour)
	c.setRateLimit(rateLimitNode{Remaining: 42, ResetAt: reset})
	rl := c.rateLimitState()
	if !rl.valid || rl.remaining != 42 || !rl.resetAt.Equal(reset) {
		t.Fatalf("snapshot mismatch: %+v", rl)
	}
}

func TestRateLimitedNow(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	cases := []struct {
		name string
		rl   rateLimitSnapshot
		want bool
	}{
		{"no reading yet", rateLimitSnapshot{}, false},
		{"low budget, window open", rateLimitSnapshot{remaining: rateLimitBackoffThreshold - 1, resetAt: future, valid: true}, true},
		{"ample budget", rateLimitSnapshot{remaining: rateLimitBackoffThreshold + 1, resetAt: future, valid: true}, false},
		{"low budget but window reset", rateLimitSnapshot{remaining: 1, resetAt: past, valid: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel()
			m.conn.rl = tc.rl
			if _, got := m.rateLimitedNow(); got != tc.want {
				t.Errorf("rateLimitedNow = %v, want %v", got, tc.want)
			}
		})
	}
}

// A nil conn (defensive: some test-constructed models omit it) must not panic
// and never reports a backoff.
func TestRateLimitedNowNilConn(t *testing.T) {
	var m model
	if _, got := m.rateLimitedNow(); got {
		t.Error("nil conn should never report rate-limited")
	}
	if m.rateLimitNotice() != "" {
		t.Error("nil conn should yield no notice")
	}
}

func TestRateLimitNotice(t *testing.T) {
	reset := time.Now().Add(30 * time.Minute)

	m := newModel()
	m.conn.setRateLimit(rateLimitNode{Remaining: rateLimitBackoffThreshold - 1, ResetAt: reset})
	notice := m.rateLimitNotice()
	if !strings.Contains(notice, "rate limit low") {
		t.Errorf("notice missing label: %q", notice)
	}
	if want := reset.Local().Format("15:04"); !strings.Contains(notice, want) {
		t.Errorf("notice %q missing resume time %q", notice, want)
	}

	// Ample budget → no notice.
	m2 := newModel()
	m2.conn.setRateLimit(rateLimitNode{Remaining: rateLimitBackoffThreshold + 1, ResetAt: reset})
	if got := m2.rateLimitNotice(); got != "" {
		t.Errorf("expected no notice with ample budget, got %q", got)
	}
}

// The notice is rendered into the status bar when backed off.
func TestStatusBarShowsRateLimitNotice(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{
		issues: []list.Item{item{number: 1, title: "a", type_: "issue"}},
		prs:    []list.Item{item{number: 2, title: "b", type_: "pr"}},
	})
	mm := tm.(model)
	mm.conn.setRateLimit(rateLimitNode{Remaining: 1, ResetAt: time.Now().Add(time.Hour)})

	bar := mm.renderStatusBar()
	if !strings.Contains(bar, "rate limit low") {
		t.Errorf("status bar missing rate-limit notice: %q", bar)
	}
	// The right-side help hint must still be present alongside the notice.
	if !strings.Contains(bar, "? help") {
		t.Errorf("status bar dropped help hint while showing notice: %q", bar)
	}
}

// An auto-refresh tick issues a list fetch normally, but skips it entirely while
// backed off on a low budget (the ticker still re-arms regardless).
func TestTickGateSkipsFetchWhenRateLimited(t *testing.T) {
	orig := fetchIssuesAndPRs
	t.Cleanup(func() { fetchIssuesAndPRs = orig })
	var fetches int
	fetchIssuesAndPRs = func(conn *githubConn) tea.Cmd {
		fetches++
		return func() tea.Msg { return nil }
	}

	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(dataMsg{issues: []list.Item{item{number: 1, title: "a", type_: "issue"}}})

	// Ample budget: the tick fetches.
	mm := tm.(model)
	mm.conn.setRateLimit(rateLimitNode{Remaining: rateLimitBackoffThreshold + 1, ResetAt: time.Now().Add(time.Hour)})
	fetches = 0
	if _, cmd := mm.Update(tickMsg(time.Now())); cmd == nil {
		t.Fatal("tick produced no cmd (expected at least the re-armed ticker)")
	}
	if fetches != 1 {
		t.Errorf("ample-budget tick: got %d fetches, want 1", fetches)
	}

	// Low budget, window open: the tick skips the fetch.
	mm.conn.setRateLimit(rateLimitNode{Remaining: 1, ResetAt: time.Now().Add(time.Hour)})
	fetches = 0
	if _, cmd := mm.Update(tickMsg(time.Now())); cmd == nil {
		t.Fatal("backed-off tick must still re-arm the ticker")
	}
	if fetches != 0 {
		t.Errorf("backed-off tick: got %d fetches, want 0", fetches)
	}
}
