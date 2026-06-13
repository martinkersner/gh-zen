package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const sampleDiff = `diff --git a/foo.go b/foo.go
index 111..222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
-old line
+new line one
+new line two
 trailing
diff --git a/added.txt b/added.txt
new file mode 100644
index 000..333
--- /dev/null
+++ b/added.txt
@@ -0,0 +1,2 @@
+alpha
+beta
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 444..000
--- a/gone.txt
+++ /dev/null
@@ -1,1 +0,0 @@
-removed
diff --git a/old/name.go b/new/name.go
similarity index 90%
rename from old/name.go
rename to new/name.go
`

func TestParseDiffEmpty(t *testing.T) {
	if got := parseDiff(""); got != nil {
		t.Errorf("parseDiff(\"\") = %v, want nil", got)
	}
	if got := parseDiff("   \n  "); got != nil {
		t.Errorf("parseDiff(whitespace) = %v, want nil", got)
	}
}

func TestParseDiffFilesAndStatuses(t *testing.T) {
	files := parseDiff(sampleDiff)
	if len(files) != 4 {
		t.Fatalf("parsed %d files, want 4: %+v", len(files), files)
	}

	want := []struct {
		path   string
		status fileStatus
		adds   int
		dels   int
	}{
		{"foo.go", statusModified, 2, 1},
		{"added.txt", statusAdded, 2, 0},
		{"gone.txt", statusDeleted, 0, 1},
		{"new/name.go", statusRenamed, 0, 0},
	}
	for i, w := range want {
		f := files[i]
		if f.path != w.path {
			t.Errorf("file %d path = %q, want %q", i, f.path, w.path)
		}
		if f.status != w.status {
			t.Errorf("file %d status = %v, want %v", i, f.status, w.status)
		}
		if f.adds != w.adds || f.dels != w.dels {
			t.Errorf("file %d counts = +%d -%d, want +%d -%d", i, f.adds, f.dels, w.adds, w.dels)
		}
	}
	if files[3].oldPath != "old/name.go" {
		t.Errorf("rename oldPath = %q, want old/name.go", files[3].oldPath)
	}
}

func TestParseDiffHunkLines(t *testing.T) {
	files := parseDiff(sampleDiff)
	h := files[0].hunks
	if len(h) != 1 {
		t.Fatalf("foo.go hunks = %d, want 1", len(h))
	}
	lines := h[0].lines
	// package main (ctx), -old line, +new one, +new two, trailing (ctx)
	if len(lines) != 5 {
		t.Fatalf("hunk lines = %d, want 5: %+v", len(lines), lines)
	}
	if lines[0].kind != lineContext || lines[0].text != "package main" {
		t.Errorf("line0 = %+v", lines[0])
	}
	if lines[1].kind != lineDel || lines[1].text != "old line" {
		t.Errorf("line1 = %+v", lines[1])
	}
	if lines[2].kind != lineAdd || lines[2].text != "new line one" {
		t.Errorf("line2 = %+v", lines[2])
	}
}

func TestRenderUnifiedOffsets(t *testing.T) {
	files := parseDiff(sampleDiff)
	out, offsets := renderUnified(files, 80)
	if len(offsets) != len(files) {
		t.Fatalf("offsets len = %d, want %d", len(offsets), len(files))
	}
	lines := strings.Split(out, "\n")
	// Each offset should point at the file's header line (contains its path).
	for i, f := range files {
		off := offsets[i]
		if off < 0 || off >= len(lines) {
			t.Fatalf("file %d offset %d out of range (%d lines)", i, off, len(lines))
		}
		plain := ansi.Strip(lines[off])
		if !strings.Contains(plain, f.path) {
			t.Errorf("file %d header line %q does not contain path %q", i, plain, f.path)
		}
	}
}

func TestRenderSideBySideAlignsAndPairs(t *testing.T) {
	files := parseDiff(sampleDiff)
	out, offsets := renderSideBySide(files, 100)
	if len(offsets) != len(files) {
		t.Fatalf("offsets len = %d, want %d", len(offsets), len(files))
	}
	plain := ansi.Strip(out)
	// foo.go's deletion and first addition should share a row (old | new).
	if !strings.Contains(plain, "old line") || !strings.Contains(plain, "new line one") {
		t.Errorf("side-by-side dropped changed text: %q", plain)
	}
	if !strings.Contains(plain, "│") {
		t.Errorf("side-by-side missing column separator: %q", plain)
	}
	// The del and the paired add must be on the same visual line.
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "old line") {
			if !strings.Contains(line, "new line one") {
				t.Errorf("del/add not paired on one row: %q", line)
			}
		}
	}
}

func TestRenderSideBySideNarrowFallsBackToUnified(t *testing.T) {
	files := parseDiff(sampleDiff)
	narrow := minColumnWidth*2 + sideBySideGap - 1
	split, _ := renderSideBySide(files, narrow)
	unified, _ := renderUnified(files, narrow)
	if split != unified {
		t.Errorf("narrow side-by-side did not fall back to unified")
	}
	if canSplit(narrow) {
		t.Errorf("canSplit(%d) = true, want false", narrow)
	}
	if !canSplit(minColumnWidth*2 + sideBySideGap) {
		t.Errorf("canSplit at exact threshold = false, want true")
	}
}

func TestRenderFileOverview(t *testing.T) {
	files := parseDiff(sampleDiff)
	out := renderFileOverview(files, 1, 100)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "4 files changed") {
		t.Errorf("overview missing summary: %q", plain)
	}
	for _, f := range files {
		if !strings.Contains(plain, f.path) {
			t.Errorf("overview missing file %q: %q", f.path, plain)
		}
	}
	if !strings.Contains(plain, "modified") || !strings.Contains(plain, "added") || !strings.Contains(plain, "deleted") || !strings.Contains(plain, "renamed") {
		t.Errorf("overview missing a status label: %q", plain)
	}
	// The active file (index 1 = added.txt) is marked.
	if !strings.Contains(plain, "▸") {
		t.Errorf("overview missing active marker: %q", plain)
	}
}

func TestRenderFileOverviewEmpty(t *testing.T) {
	out := renderFileOverview(nil, 0, 80)
	if !strings.Contains(ansi.Strip(out), "No files changed") {
		t.Errorf("empty overview = %q", out)
	}
}

func TestParseDiffToleratesBinaryNotice(t *testing.T) {
	in := "diff --git a/img.png b/img.png\nnew file mode 100644\nindex 0..1\nBinary files /dev/null and b/img.png differ\n"
	files := parseDiff(in)
	if len(files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(files))
	}
	if files[0].status != statusAdded {
		t.Errorf("binary file status = %v, want added", files[0].status)
	}
	if len(files[0].hunks) != 0 {
		t.Errorf("binary file should have no hunks, got %d", len(files[0].hunks))
	}
}

// maxVisibleWidth returns the widest visible (ANSI-stripped) row in s.
func maxVisibleWidth(s string) int {
	w := 0
	for _, ln := range strings.Split(s, "\n") {
		if vw := ansi.StringWidth(ln); vw > w {
			w = vw
		}
	}
	return w
}

// longDiff is a one-file diff whose first content line is far wider than any
// test viewport, used to exercise soft-wrapping and offset accounting.
const longDiff = `diff --git a/long.go b/long.go
--- a/long.go
+++ b/long.go
@@ -1,2 +1,2 @@
-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
+bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
 tail
`

// renderUnified soft-wraps a long content line across multiple physical rows
// instead of clipping it, and no rendered row exceeds the width.
func TestRenderUnifiedWrapsLongLine(t *testing.T) {
	files := parseDiff(longDiff)
	const width = 20
	out, _ := renderUnified(files, width)
	plain := ansi.Strip(out)
	// Every wrapped content row (the 'a'/'b' runs) must fit within width; the
	// per-file/hunk header lines are not content and aren't wrapped by this view.
	for _, ln := range strings.Split(plain, "\n") {
		if !strings.ContainsAny(ln, "ab") {
			continue
		}
		if w := ansi.StringWidth(ln); w > width {
			t.Errorf("content row %q is %d cols wide, want <= %d (not wrapped/clipped)", ln, w, width)
		}
	}
	// All of the long line's content must survive (nothing dropped to a "…").
	if strings.Contains(plain, "…") {
		t.Errorf("content was clipped with an ellipsis instead of wrapped: %q", plain)
	}
	full := strings.ReplaceAll(plain, "\n", "")
	if !strings.Contains(full, strings.Repeat("a", 74)) {
		t.Errorf("wrapped deletion text incomplete: %q", plain)
	}
	if !strings.Contains(full, strings.Repeat("b", 74)) {
		t.Errorf("wrapped addition text incomplete: %q", plain)
	}
}

// When a content line wraps, file-header offsets for later files still point at
// the correct rendered rows (the wrap shifts subsequent rows, and the offset
// must track rendered rows, not logical lines).
func TestRenderUnifiedOffsetsAfterWrap(t *testing.T) {
	// Two files; the first has a long wrapping line, so the second file's header
	// offset is past the extra wrapped rows.
	in := longDiff + "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n+short\n"
	files := parseDiff(in)
	if len(files) != 2 {
		t.Fatalf("parsed %d files, want 2", len(files))
	}
	const width = 20
	out, offsets := renderUnified(files, width)
	lines := strings.Split(out, "\n")
	for i, f := range files {
		off := offsets[i]
		if off < 0 || off >= len(lines) {
			t.Fatalf("file %d offset %d out of range (%d lines)", i, off, len(lines))
		}
		if !strings.Contains(ansi.Strip(lines[off]), f.path) {
			t.Errorf("file %d offset %d points at %q, want header for %q", i, off, ansi.Strip(lines[off]), f.path)
		}
	}
}

// The +/- marker and per-kind color are preserved on a wrapped line: the marker
// is on the first row and the deletion color styles its rows.
func TestRenderUnifiedWrapMarkerAndColor(t *testing.T) {
	dl := diffLine{kind: lineDel, text: strings.Repeat("x", 60)}
	const width = 20
	rows := renderDiffContentLine(dl, width)
	if len(rows) < 2 {
		t.Fatalf("expected the long line to wrap into multiple rows, got %d", len(rows))
	}
	if !strings.HasPrefix(ansi.Strip(rows[0]), "-") {
		t.Errorf("first row missing del marker: %q", ansi.Strip(rows[0]))
	}
	// Continuation rows carry text only (no extra marker injected).
	if strings.HasPrefix(ansi.Strip(rows[1]), "-") {
		t.Errorf("continuation row should not repeat the marker: %q", ansi.Strip(rows[1]))
	}
	// Every row reassembles to the full original line, and each row is produced by
	// applying diffDelStyle to its hard-wrapped plain slice (so styling is per row,
	// not just the first). Color escapes are stripped in non-TTY test runs, so we
	// assert structural equality against an independently computed expectation.
	wantPlain := strings.Split(ansi.Hardwrap("-"+dl.text, width, true), "\n")
	if len(rows) != len(wantPlain) {
		t.Fatalf("got %d rows, want %d", len(rows), len(wantPlain))
	}
	for i, r := range rows {
		if want := diffDelStyle.Render(wantPlain[i]); r != want {
			t.Errorf("row %d = %q, want %q (per-row deletion styling)", i, r, want)
		}
		if w := ansi.StringWidth(r); w > width {
			t.Errorf("row %d width %d > %d", i, w, width)
		}
	}
}

// A space at a wrap boundary (indentation / a separator between tokens) must
// survive on the continuation row rather than being silently dropped, so the
// reassembled rows reproduce the original source line exactly.
func TestRenderDiffContentLineWrapPreservesSpaces(t *testing.T) {
	const width = 6
	text := "foofoo    bar" // 4 spaces of "indentation" straddle the wrap boundary
	dl := diffLine{kind: lineContext, text: text}
	rows := renderDiffContentLine(dl, width)
	if len(rows) < 2 {
		t.Fatalf("expected wrapping, got %d row(s)", len(rows))
	}
	var got strings.Builder
	for _, r := range rows {
		got.WriteString(ansi.Strip(r))
	}
	// Context line has a leading space marker, so the reassembled text is " "+text.
	if want := " " + text; got.String() != want {
		t.Errorf("wrapped+reassembled = %q, want %q (boundary spaces dropped)", got.String(), want)
	}
}

// Side-by-side wraps within each column width: no rendered row exceeds the full
// width and the column separator stays present.
func TestRenderSideBySideWrapsWithinColumns(t *testing.T) {
	files := parseDiff(longDiff)
	const width = 60 // wide enough to split: two ~28-col columns
	if !canSplit(width) {
		t.Fatalf("canSplit(%d) = false; pick a wider test width", width)
	}
	out, offsets := renderSideBySide(files, width)
	if len(offsets) != len(files) {
		t.Fatalf("offsets len = %d, want %d", len(offsets), len(files))
	}
	if w := maxVisibleWidth(out); w > width {
		t.Errorf("a side-by-side row is %d cols wide, want <= %d: %q", w, width, ansi.Strip(out))
	}
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "│") {
		t.Errorf("side-by-side missing separator: %q", plain)
	}
	if strings.Contains(plain, "…") {
		t.Errorf("side-by-side clipped instead of wrapping: %q", plain)
	}
	// The deletion ('a's) lives in the left column and the addition ('b's) in the
	// right column, each split across rows. Reassemble per column and confirm the
	// full 74-char runs survive the wrap.
	col := (width - sideBySideGap) / 2
	var leftCol, rightCol strings.Builder
	for _, ln := range strings.Split(plain, "\n") {
		parts := strings.SplitN(ln, "│", 2)
		if len(parts) != 2 {
			continue
		}
		leftCol.WriteString(strings.Trim(parts[0], " "))
		rightCol.WriteString(strings.Trim(parts[1], " "))
	}
	if !strings.Contains(leftCol.String(), strings.Repeat("a", 74)) {
		t.Errorf("left column dropped wrapped deletion (col=%d): %q", col, leftCol.String())
	}
	if !strings.Contains(rightCol.String(), strings.Repeat("b", 74)) {
		t.Errorf("right column dropped wrapped addition (col=%d): %q", col, rightCol.String())
	}
}
