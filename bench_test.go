package main

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
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

const (
	// benchTermWidth/benchTermHeight match the e2e default terminal size so
	// the measured layout work is representative.
	benchTermWidth  = 80
	benchTermHeight = 24
	// benchWaitTimeout bounds each teatest WaitFor inside a benchmark iteration
	// so a missed render fails the bench fast rather than hanging it. Generous
	// vs. the in-memory render loop.
	benchWaitTimeout = 3 * time.Second
	// benchFinalTimeout bounds program shutdown after Quit in a teatest bench.
	benchFinalTimeout = 3 * time.Second
)

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
	base := seededModel(b, benchTermWidth, benchTermHeight)
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var tm tea.Model = base
		tm, _ = tm.Update(enter)
		tm, _ = tm.Update(esc)
		_ = tm
	}
}

// BenchmarkUpdateNavigate measures cursor movement through Update on the list.
func BenchmarkUpdateNavigate(b *testing.B) {
	base := seededModel(b, benchTermWidth, benchTermHeight)
	down := tea.KeyMsg{Type: tea.KeyCtrlN}
	up := tea.KeyMsg{Type: tea.KeyCtrlP}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var tm tea.Model = base
		tm, _ = tm.Update(down)
		tm, _ = tm.Update(up)
		_ = tm
	}
}

// BenchmarkViewList measures rendering the list screen.
func BenchmarkViewList(b *testing.B) {
	m := seededModel(b, benchTermWidth, benchTermHeight)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkViewDetail measures rendering the detail screen.
func BenchmarkViewDetail(b *testing.B) {
	m := openedDetailModel(b, benchTermWidth, benchTermHeight)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// stubFetchBench swaps the network-backed fetch cmds for hermetic ones for the
// duration of the benchmark, restoring them on cleanup. Mirrors stubFetch (the
// *testing.T helper) so teatest benchmarks never touch the network.
func stubFetchBench(b *testing.B) {
	b.Helper()
	origFetch := fetchIssuesAndPRs
	origDiff := ghDiff
	fetchIssuesAndPRs = func() tea.Cmd {
		return func() tea.Msg { return seedData() }
	}
	ghDiff = func(int) (string, error) { return "+added line\n-removed line\n", nil }
	b.Cleanup(func() {
		fetchIssuesAndPRs = origFetch
		ghDiff = origDiff
	})
}

// newSeededProgram starts a teatest program whose list is populated by the
// stubbed fetch (no network), the program analogue of newSeededModel.
func newSeededProgram(b *testing.B) *teatest.TestModel {
	b.Helper()
	return teatest.NewTestModel(b, newModel(), teatest.WithInitialTermSize(benchTermWidth, benchTermHeight))
}

// waitForBench waits until the output contains needle, bounded by
// benchWaitTimeout. teatest.WaitFor accepts the testing.TB interface, so it
// works from a benchmark.
func waitForBench(b *testing.B, tm *teatest.TestModel, needle string) {
	b.Helper()
	teatest.WaitFor(b, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte(needle))
	}, teatest.WithDuration(benchWaitTimeout))
}

// BenchmarkLaunch measures cold launch to first render: start the program and
// wait for the seeded list to render, then tear it down. Each iteration is a
// full bootstrap (Init load + first frames) through the in-memory terminal.
func BenchmarkLaunch(b *testing.B) {
	stubFetchBench(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm := newSeededProgram(b)
		waitForBench(b, tm, "first issue alpha")
		b.StopTimer()
		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
		tm.WaitFinished(b, teatest.WithFinalTimeout(benchFinalTimeout))
		b.StartTimer()
	}
}

// BenchmarkTransitionListDetail measures the round trip list -> detail -> list
// driven end-to-end through the running program. Setup (launch + initial list
// render) is excluded from the timer; only the transition waits are timed.
func BenchmarkTransitionListDetail(b *testing.B) {
	stubFetchBench(b)
	tm := newSeededProgram(b)
	waitForBench(b, tm, "first issue alpha")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForBench(b, tm, "#11 first issue alpha")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		waitForBench(b, tm, "second issue beta")
	}
	b.StopTimer()
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(b, teatest.WithFinalTimeout(benchFinalTimeout))
}

// BenchmarkTransitionHelpOverlay measures opening and closing the shortcuts
// overlay end-to-end.
func BenchmarkTransitionHelpOverlay(b *testing.B) {
	stubFetchBench(b)
	tm := newSeededProgram(b)
	waitForBench(b, tm, "first issue alpha")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		waitForBench(b, tm, "Keyboard shortcuts")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		waitForBench(b, tm, "first issue alpha")
	}
	b.StopTimer()
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(b, teatest.WithFinalTimeout(benchFinalTimeout))
}

// BenchmarkTransitionFilter measures applying a filter end-to-end: open filter,
// type a query that narrows the list, then clear it.
func BenchmarkTransitionFilter(b *testing.B) {
	stubFetchBench(b)
	tm := newSeededProgram(b)
	waitForBench(b, tm, "first issue alpha")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		tm.Type("beta")
		waitForBench(b, tm, "second issue beta")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		waitForBench(b, tm, "first issue alpha")
	}
	b.StopTimer()
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(b, teatest.WithFinalTimeout(benchFinalTimeout))
}
