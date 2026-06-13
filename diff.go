package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fileStatus is the change kind of a single file in a diff, derived from the
// git "diff --git" header lines (new file / deleted file / rename) rather than
// from the +/- line tally.
type fileStatus int

const (
	statusModified fileStatus = iota
	statusAdded
	statusDeleted
	statusRenamed
)

func (s fileStatus) label() string {
	switch s {
	case statusAdded:
		return "added"
	case statusDeleted:
		return "deleted"
	case statusRenamed:
		return "renamed"
	default:
		return "modified"
	}
}

// diffLineKind classifies a single content line within a hunk so renderers can
// color it and the side-by-side splitter can route it to the old/new column.
type diffLineKind int

const (
	lineContext diffLineKind = iota
	lineAdd
	lineDel
)

// diffLine is one content line of a hunk: its kind and the text after the
// leading +/-/space marker is stripped.
type diffLine struct {
	kind diffLineKind
	text string
}

// hunk is a single @@ section of a file diff: the raw header plus its content
// lines (context/add/del), markers stripped.
type hunk struct {
	header string
	lines  []diffLine
}

// fileDiff is one file's worth of a unified diff: its display path, change
// status, +adds/-dels tally, and parsed hunks. path is the new path for
// modified/added files and the post-rename path for renames; oldPath is set
// only for renames.
type fileDiff struct {
	path    string
	oldPath string
	status  fileStatus
	adds    int
	dels    int
	hunks   []hunk
}

// parseDiff parses a unified git diff (the output of `gh pr diff`) into a slice
// of per-file diffs. It is pure and deterministic so renderers and tests can
// rely on it. Lines outside any recognized file/hunk structure (e.g. a binary
// notice) are tolerated and skipped; a file with no hunks (e.g. pure rename or
// binary) still appears in the result with its status and zero counts.
func parseDiff(diff string) []fileDiff {
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	var files []fileDiff
	var cur *fileDiff
	var curHunk *hunk

	flushHunk := func() {
		if cur != nil && curHunk != nil {
			cur.hunks = append(cur.hunks, *curHunk)
			curHunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for _, ln := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			flushFile()
			cur = &fileDiff{status: statusModified, path: parseGitHeaderPath(ln)}
		case cur == nil:
			// Preamble before the first file header; ignore.
			continue
		case strings.HasPrefix(ln, "new file"):
			cur.status = statusAdded
		case strings.HasPrefix(ln, "deleted file"):
			cur.status = statusDeleted
		case strings.HasPrefix(ln, "rename from "):
			cur.status = statusRenamed
			cur.oldPath = strings.TrimPrefix(ln, "rename from ")
		case strings.HasPrefix(ln, "rename to "):
			cur.status = statusRenamed
			cur.path = strings.TrimPrefix(ln, "rename to ")
		case strings.HasPrefix(ln, "--- "):
			// Old-file marker; the new-file marker carries the authoritative path.
			continue
		case strings.HasPrefix(ln, "+++ "):
			if p := pathFromMarker(ln); p != "" {
				cur.path = p
			}
		case strings.HasPrefix(ln, "@@"):
			flushHunk()
			curHunk = &hunk{header: ln}
		case curHunk != nil && strings.HasPrefix(ln, "+"):
			cur.adds++
			curHunk.lines = append(curHunk.lines, diffLine{kind: lineAdd, text: ln[1:]})
		case curHunk != nil && strings.HasPrefix(ln, "-"):
			cur.dels++
			curHunk.lines = append(curHunk.lines, diffLine{kind: lineDel, text: ln[1:]})
		case curHunk != nil && strings.HasPrefix(ln, " "):
			curHunk.lines = append(curHunk.lines, diffLine{kind: lineContext, text: ln[1:]})
		case curHunk != nil && ln == "":
			// A blank line inside a hunk is a context line with empty text.
			curHunk.lines = append(curHunk.lines, diffLine{kind: lineContext, text: ""})
		default:
			// Index lines, mode lines, "\ No newline at end of file", binary
			// notices, etc. — tolerated and skipped.
			continue
		}
	}
	flushFile()
	return files
}

// parseGitHeaderPath extracts a best-effort display path from a
// "diff --git a/x b/y" header. Used as the initial path before the +++ marker
// (or rename headers) refine it. Returns "" if it can't be parsed.
func parseGitHeaderPath(ln string) string {
	rest := strings.TrimPrefix(ln, "diff --git ")
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	// Prefer the "b/" side (the new path); fall back to the first field.
	last := fields[len(fields)-1]
	return strings.TrimPrefix(last, "b/")
}

// pathFromMarker extracts the path from a "+++ b/path" marker line, stripping
// the "b/" prefix. Returns "" for /dev/null (a deleted file).
func pathFromMarker(ln string) string {
	p := strings.TrimSpace(strings.TrimPrefix(ln, "+++ "))
	if p == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(p, "b/")
}

// diff render styles, shared by the unified, side-by-side, and overview
// renderers so all three read consistently with the rest of the TUI.
var (
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))
	diffDelStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e"))
	diffMetaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7"))
	diffPathStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#bb9af7"))
	diffMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	diffActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e0af68"))
)

// fileHeaderLine renders the one-line per-file header used at the top of each
// file's block in both the unified and side-by-side views (e.g.
// "modified path/to/file  +3 -1"). It is the navigation anchor that file
// stepping ([ / ]) scrolls to.
func (f fileDiff) headerLine() string {
	name := f.path
	if f.status == statusRenamed && f.oldPath != "" && f.oldPath != f.path {
		name = f.oldPath + " → " + f.path
	}
	if name == "" {
		name = "(unknown)"
	}
	return diffMutedStyle.Render(f.status.label()) + " " +
		diffPathStyle.Render(name) + "  " +
		diffAddStyle.Render(fmt.Sprintf("+%d", f.adds)) + " " +
		diffDelStyle.Render(fmt.Sprintf("-%d", f.dels))
}

// renderUnified renders all files as a unified diff with a per-file header
// above each file's hunks. It returns the rendered text plus, for each file,
// the 0-based line offset of that file's header within the text (used by file
// navigation to scroll to a file).
func renderUnified(files []fileDiff, width int) (string, []int) {
	var b strings.Builder
	offsets := make([]int, 0, len(files))
	line := 0
	emit := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		line++
	}
	for fi, f := range files {
		if fi > 0 {
			emit("")
		}
		offsets = append(offsets, line)
		emit(f.headerLine())
		for _, h := range f.hunks {
			emit(diffMetaStyle.Render(h.header))
			for _, dl := range h.lines {
				// A long content line soft-wraps into multiple physical rows;
				// emit each so the line counter (and thus file offsets) tracks
				// rendered rows, keeping navigation aligned (see refreshDiffView).
				for _, row := range renderDiffContentLine(dl, width) {
					emit(row)
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), offsets
}

// renderDiffContentLine renders one hunk content line with its +/-/space marker
// restored and colored by kind. When width > 0 the line is soft-wrapped to that
// width and returned as one styled string per physical row (so callers can count
// rendered rows for the per-file line accounting used by navigation); the marker
// sits on the first row and continuation rows carry only wrapped text. Coloring
// is applied per row so styling survives the wrap. width <= 0 yields a single
// unwrapped row.
func renderDiffContentLine(dl diffLine, width int) []string {
	var marker string
	var style lipgloss.Style
	switch dl.kind {
	case lineAdd:
		marker, style = "+", diffAddStyle
	case lineDel:
		marker, style = "-", diffDelStyle
	default:
		marker, style = " ", lipgloss.NewStyle()
	}
	text := marker + dl.text
	return wrapStyled(text, style, width)
}

// wrapStyled hard-wraps plain text to width visible columns and styles each
// resulting physical row, returning one string per row. width <= 0 (or text that
// fits) yields a single row. Wrapping happens on the unstyled text so the wrap
// math isn't thrown off by color escapes; styling is then applied per row.
func wrapStyled(text string, style lipgloss.Style, width int) []string {
	if width <= 0 || ansi.StringWidth(text) <= width {
		return []string{style.Render(text)}
	}
	rows := strings.Split(ansi.Hardwrap(text, width, false), "\n")
	for i, r := range rows {
		rows[i] = style.Render(r)
	}
	return rows
}

// minColumnWidth is the smallest per-column width at which the side-by-side
// view is legible; below twice this (plus the separator) the renderers fall
// back to unified.
const minColumnWidth = 20

// canSplit reports whether width can fit two side-by-side columns at the
// minimum legible width.
func canSplit(width int) bool {
	return width >= minColumnWidth*2+sideBySideGap
}

// sideBySideGap is the visible width of the column separator (" │ ").
const sideBySideGap = 3

// renderSideBySide renders all files with old (left) and new (right) columns.
// Within each hunk, deletions align on the left, additions on the right, and
// context lines appear on both. Returns the rendered text plus each file's
// header line offset (same contract as renderUnified). When the terminal is too
// narrow for two columns it falls back to renderUnified.
func renderSideBySide(files []fileDiff, width int) (string, []int) {
	if !canSplit(width) {
		return renderUnified(files, width)
	}
	col := (width - sideBySideGap) / 2
	sep := diffMutedStyle.Render(" │ ")

	var b strings.Builder
	offsets := make([]int, 0, len(files))
	line := 0
	emit := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
		line++
	}
	for fi, f := range files {
		if fi > 0 {
			emit("")
		}
		offsets = append(offsets, line)
		emit(f.headerLine())
		for _, h := range f.hunks {
			emit(diffMetaStyle.Render(h.header))
			for _, row := range pairHunkLines(h.lines) {
				// Each cell soft-wraps to its column width independently; the two
				// sides may produce a different number of physical rows, so pad
				// the shorter side with blank cells and emit one joined row per
				// physical line. The line counter advances per emitted row so file
				// offsets track rendered rows (see refreshDiffView).
				left := renderColumnCell(row.left, lineDel, col)
				right := renderColumnCell(row.right, lineAdd, col)
				n := len(left)
				if len(right) > n {
					n = len(right)
				}
				blank := strings.Repeat(" ", col)
				for k := 0; k < n; k++ {
					l, r := blank, blank
					if k < len(left) {
						l = left[k]
					}
					if k < len(right) {
						r = right[k]
					}
					emit(l + sep + r)
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n"), offsets
}

// sideRow is one rendered row of the side-by-side view: the old-side cell (left)
// and new-side cell (right). A nil pointer means that side is blank for the row.
type sideRow struct {
	left  *diffLine
	right *diffLine
}

// pairHunkLines turns a hunk's sequential content lines into side-by-side rows.
// Context lines occupy both columns. A run of deletions is paired against the
// run of additions that immediately follows it (line i of the run on the left
// against line i on the right); leftover deletions/additions get a blank
// opposite cell. This keeps changed lines visually adjacent.
func pairHunkLines(lines []diffLine) []sideRow {
	var rows []sideRow
	i := 0
	for i < len(lines) {
		switch lines[i].kind {
		case lineContext:
			ln := lines[i]
			rows = append(rows, sideRow{left: &ln, right: &ln})
			i++
		default:
			// Collect a maximal run of dels then a run of adds.
			var dels, adds []diffLine
			for i < len(lines) && lines[i].kind == lineDel {
				dels = append(dels, lines[i])
				i++
			}
			for i < len(lines) && lines[i].kind == lineAdd {
				adds = append(adds, lines[i])
				i++
			}
			n := len(dels)
			if len(adds) > n {
				n = len(adds)
			}
			for k := 0; k < n; k++ {
				var row sideRow
				if k < len(dels) {
					d := dels[k]
					row.left = &d
				}
				if k < len(adds) {
					a := adds[k]
					row.right = &a
				}
				rows = append(rows, row)
			}
		}
	}
	return rows
}

// renderColumnCell renders one side's cell (no marker — the column position
// conveys old vs new), colored by kind for changed lines and uncolored for
// context, soft-wrapped to width and right-padded so every physical row is
// exactly width visible columns. Returns one string per physical row; a nil line
// yields a single blank row.
func renderColumnCell(dl *diffLine, fallbackKind diffLineKind, width int) []string {
	blank := strings.Repeat(" ", width)
	if dl == nil {
		return []string{blank}
	}
	var style lipgloss.Style
	switch dl.kind {
	case lineAdd:
		style = diffAddStyle
	case lineDel:
		style = diffDelStyle
	default:
		style = lipgloss.NewStyle()
	}
	plainRows := strings.Split(ansi.Hardwrap(dl.text, width, false), "\n")
	rows := make([]string, len(plainRows))
	for i, r := range plainRows {
		// Pad to full column width on the visible (unstyled) text so the
		// separator and right column align regardless of color escapes.
		pad := width - ansi.StringWidth(r)
		if pad < 0 {
			pad = 0
		}
		rows[i] = style.Render(r) + strings.Repeat(" ", pad)
	}
	return rows
}

// renderFileOverview renders the changed-files overview: one row per file with
// its status and +adds/-dels counts, the active file (the one navigation last
// jumped to) highlighted. Width is used to keep rows from wrapping.
func renderFileOverview(files []fileDiff, active, width int) string {
	if len(files) == 0 {
		return diffMutedStyle.Render("No files changed.")
	}
	var totAdds, totDels int
	for _, f := range files {
		totAdds += f.adds
		totDels += f.dels
	}
	title := diffPathStyle.Render(fmt.Sprintf("%d files changed", len(files))) + "  " +
		diffAddStyle.Render(fmt.Sprintf("+%d", totAdds)) + " " +
		diffDelStyle.Render(fmt.Sprintf("-%d", totDels))

	rows := []string{title, ""}
	for i, f := range files {
		name := f.path
		if f.status == statusRenamed && f.oldPath != "" && f.oldPath != f.path {
			name = f.oldPath + " → " + f.path
		}
		if name == "" {
			name = "(unknown)"
		}
		marker := "  "
		nameStyle := diffPathStyle
		if i == active {
			marker = diffActiveStyle.Render("▸ ")
			nameStyle = diffActiveStyle
		}
		counts := diffAddStyle.Render(fmt.Sprintf("+%d", f.adds)) + " " +
			diffDelStyle.Render(fmt.Sprintf("-%d", f.dels))
		status := diffMutedStyle.Render(fmt.Sprintf("%-8s", f.status.label()))
		row := marker + status + " " + nameStyle.Render(name) + "  " + counts
		if width > 0 {
			row = ansi.Truncate(row, width, "…")
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}
