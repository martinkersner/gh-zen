package main

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// Bench-only render-wait bounds. The teatest benchmarks poll the program's
// rendered output from a hot benchmark goroutine, which can starve the render
// goroutine of CPU far longer than the e2e suite's interactive 3s margin
// (e2eWaitTimeout) — especially under `make bench` (no -v, output buffered).
// These are deliberately generous: they only bound a missed render so a stuck
// run fails instead of hanging, and never enter the reported ns/op. Kept
// separate so the e2e suite keeps its tight 3s guard.
const (
	benchWaitTimeout  = 10 * time.Second
	benchFinalTimeout = 10 * time.Second
)

// Performance benchmarks for the TUI. They split into two layers:
//
//   - Synchronous Update/View micro-benchmarks (BenchmarkUpdate*, BenchmarkView*)
//     drive the model in-process via the tea.Model interface, with no program
//     loop or goroutines. These measure the cost of the hot paths directly and
//     report allocations.
//   - teatest-driven end-to-end timings (BenchmarkLaunch, BenchmarkTransition*)
//     run the full tea.Program against an in-memory terminal so a single
//     iteration measures real launch / screen-transition latency, including the
//     render loop. These reuse the offline fetch stub (stubFetch / seedData) so
//     timings reflect UI work, not network or `gh`.
//
// All of these run with `go test -bench=. -run='^$'` (see `make bench`) and
// never touch the network.

// Terminal size and timeouts reuse the e2e_test.go constants (same package,
// same values) so the bench harness stays in lockstep with the e2e harness.

// seededModel returns a model with the sample issues/PRs already loaded and the
// terminal sized, with the loading state cleared, ready to receive key input.
// It is the synchronous (non-program) analogue of newSeededModel: it bypasses
// Init's async fetch by feeding seedData straight through Update.
func seededModel(b *testing.B, w, h int) model {
	b.Helper()
	var tm tea.Model = newModel()
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
	tm, _ = tm.Update(seedData())
	mm := tm.(model)
	mm.loading = false
	return mm
}

// openedDetailModel returns a seeded model with the first issue's detail view
// already open.
func openedDetailModel(b *testing.B, w, h int) model {
	b.Helper()
	var tm tea.Model = seededModel(b, w, h)
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)
	if !mm.detailOpen {
		b.Fatal("detail did not open")
	}
	return mm
}

// BenchmarkUpdateKeyBatch measures processing a batch of tea.KeyMsg events
// through Update: open the detail view, then back to the list. This exercises
// the two heaviest transition handlers per iteration.
func BenchmarkUpdateKeyBatch(b *testing.B) {
	base := seededModel(b, e2eTermWidth, e2eTermHeight)
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	b.ReportAllocs()
	for b.Loop() {
		var tm tea.Model = base
		tm, _ = tm.Update(enter)
		tm, _ = tm.Update(esc)
		_ = tm
	}
}

// BenchmarkUpdateNavigate measures cursor movement through Update on the list.
func BenchmarkUpdateNavigate(b *testing.B) {
	base := seededModel(b, e2eTermWidth, e2eTermHeight)
	down := tea.KeyMsg{Type: tea.KeyCtrlN}
	up := tea.KeyMsg{Type: tea.KeyCtrlP}

	b.ReportAllocs()
	for b.Loop() {
		var tm tea.Model = base
		tm, _ = tm.Update(down)
		tm, _ = tm.Update(up)
		_ = tm
	}
}

// BenchmarkViewList measures rendering the list screen.
func BenchmarkViewList(b *testing.B) {
	m := seededModel(b, e2eTermWidth, e2eTermHeight)

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkViewDetail measures rendering the detail screen.
func BenchmarkViewDetail(b *testing.B) {
	m := openedDetailModel(b, e2eTermWidth, e2eTermHeight)

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

// stubFetchBench swaps the network-backed fetch cmds for hermetic ones for the
// duration of the benchmark, restoring them on cleanup. Mirrors stubFetch (the
// *testing.T helper) so teatest benchmarks never touch the network.
//
// It is idempotent within a single benchmark: newSeededProgram calls it once per
// iteration (so a new teatest bench can't forget to stub), but only the first
// call swaps the globals and registers the restore. This both captures the true
// originals (not an already-stubbed value) and registers exactly one cleanup,
// regardless of b.N. The guard resets on cleanup so the next benchmark re-stubs.
var benchFetchStubbed bool

func stubFetchBench(b *testing.B) {
	b.Helper()
	if benchFetchStubbed {
		return
	}
	benchFetchStubbed = true
	origFetch := fetchIssuesAndPRs
	origDiff := ghDiff
	fetchIssuesAndPRs = func(*githubConn) tea.Cmd {
		return func() tea.Msg { return seedData() }
	}
	ghDiff = func(int) (string, error) { return "+added line\n-removed line\n", nil }
	b.Cleanup(func() {
		fetchIssuesAndPRs = origFetch
		ghDiff = origDiff
		benchFetchStubbed = false
	})
}

// newSeededProgram installs the offline fetch stub (so no network is touched)
// and starts a teatest program whose list is populated by it — the program
// analogue of newSeededModel. The stub is installed here rather than left to the
// caller so a new teatest benchmark can't silently hit the network by forgetting
// to stub.
func newSeededProgram(b *testing.B) *teatest.TestModel {
	b.Helper()
	stubFetchBench(b)
	return teatest.NewTestModel(b, newModel(), teatest.WithInitialTermSize(e2eTermWidth, e2eTermHeight))
}

// waitForBench waits until the program's output contains needle, bounded by
// benchWaitTimeout. tm.Output() returns the same consuming reader each call, so
// each WaitFor only sees bytes written since the previous read — callers must
// pass a needle that the transition under test freshly re-renders. teatest.WaitFor
// accepts the testing.TB interface, so it works from a benchmark.
func waitForBench(b *testing.B, tm *teatest.TestModel, needle string) {
	b.Helper()
	teatest.WaitFor(b, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(needle))
	}, teatest.WithDuration(benchWaitTimeout))
}

// quitBench tears down a teatest program and waits for it to finish, bounded by
// a timeout so a stuck shutdown fails the benchmark loudly instead of hanging.
func quitBench(b *testing.B, tm *teatest.TestModel) {
	b.Helper()
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(b, teatest.WithFinalTimeout(benchFinalTimeout))
}

// The teatest transition benchmarks below all spin up a fresh program per timed
// iteration and tear it down at the end of the same iteration. A fresh program
// avoids the cross-iteration pitfall of reusing one tm: tm.Output() is a
// consuming reader, so across iterations a "back to <screen>" wait could race a
// skipped/diffed frame and either pass on stale bytes or hang. Per-iteration
// programs keep each measurement self-contained and the needle waits unambiguous,
// matching the one-shot pattern proven in e2e_test.go. Per-iteration setup
// (program start + initial list render) and teardown are excluded from the timer
// via StopTimer/StartTimer, so the reported time covers only the transition
// keystrokes and their render waits, not the bootstrap (BenchmarkLaunch already
// covers launch cost separately).

// BenchmarkLaunch measures cold launch to first render: start the program and
// wait for the seeded list to render, then tear it down. Each iteration is a
// full bootstrap (Init load + first frames) through the in-memory terminal.
// Teardown is excluded from the timer so only launch-to-first-render is measured.
func BenchmarkLaunch(b *testing.B) {
	for b.Loop() {
		tm := newSeededProgram(b)
		waitForBench(b, tm, "first issue alpha")
		b.StopTimer()
		quitBench(b, tm)
		b.StartTimer()
	}
}

// BenchmarkTransitionListDetail measures the round trip list -> detail -> list
// driven end-to-end through the running program. The "back to list" wait uses
// "second issue beta" (a different item) as the sentinel: it never appears in
// the single-item detail view, so seeing it proves the list re-rendered.
func BenchmarkTransitionListDetail(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		tm := newSeededProgram(b)
		waitForBench(b, tm, "first issue alpha")
		b.StartTimer()

		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForBench(b, tm, "#11 first issue alpha")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		waitForBench(b, tm, "second issue beta")

		b.StopTimer()
		quitBench(b, tm)
		b.StartTimer()
	}
}

// BenchmarkTransitionHelpOverlay measures opening and closing the shortcuts
// overlay end-to-end.
func BenchmarkTransitionHelpOverlay(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		tm := newSeededProgram(b)
		waitForBench(b, tm, "first issue alpha")
		b.StartTimer()

		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		waitForBench(b, tm, "Keyboard shortcuts")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		// The overlay text contains no "first issue alpha", so its reappearance
		// unambiguously marks the return to the list (vs. the drained open frame).
		waitForBench(b, tm, "first issue alpha")

		b.StopTimer()
		quitBench(b, tm)
		b.StartTimer()
	}
}

// BenchmarkTransitionFilter measures applying a filter end-to-end: open filter,
// type a query that narrows the list, then clear it. The "back to list" wait
// uses "first issue alpha" — the row filtered out by "beta" — so its reappearance
// proves the filter was cleared and the full list re-rendered.
func BenchmarkTransitionFilter(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		tm := newSeededProgram(b)
		waitForBench(b, tm, "first issue alpha")
		b.StartTimer()

		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		tm.Type("beta")
		waitForBench(b, tm, "second issue beta")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		waitForBench(b, tm, "first issue alpha")

		b.StopTimer()
		quitBench(b, tm)
		b.StartTimer()
	}
}
