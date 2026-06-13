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
