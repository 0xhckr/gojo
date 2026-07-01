package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"gojo/internal/jj"
)

// splitView carries the live split-mode selection into diff panel rendering so
// each row can show its accept/partial-accept indicator.
type splitView struct {
	active bool
	marked map[int]bool // diff row indices of marked addition/deletion lines
}

// splitFinishedMsg is returned when `jj split` completes (or fails).
// selectedRev is the change ID of the newly created (selected) revision on
// success; the Update handler opens its diff so the user can inspect it.
type splitFinishedMsg struct {
	rev         string
	selectedRev string
	err         error
	elev        *elevReq
}

// splitIndicatorForRow returns the indicator glyph for a diff row in split
// mode: "[x]" for fully marked, "[~]" for partial, "[ ]" for unmarked, "" when
// split mode is inactive or the row has no selectable content.
func splitIndicatorForRow(rows []diffRow, rowIdx int, sv splitView) string {
	if !sv.active {
		return ""
	}
	r := rows[rowIdx]
	switch r.kind {
	case rowFileHeader:
		return splitFileIndicator(rows, rowIdx, sv)
	case rowLine:
		if r.lineKind != "addition" && r.lineKind != "deletion" {
			return ""
		}
		if sv.marked[rowIdx] {
			return "[x]"
		}
		return "[ ]"
	default:
		return ""
	}
}

// splitFileIndicator computes the file-level indicator by scanning all
// addition/deletion lines within the file's row range.
func splitFileIndicator(rows []diffRow, headerIdx int, sv splitView) string {
	start := headerIdx + 1
	end := len(rows)
	for j := headerIdx + 1; j < len(rows); j++ {
		if rows[j].kind == rowFileHeader {
			end = j
			break
		}
	}
	marked, total := 0, 0
	for j := start; j < end; j++ {
		if rows[j].kind == rowLine && (rows[j].lineKind == "addition" || rows[j].lineKind == "deletion") {
			total++
			if sv.marked[j] {
				marked++
			}
		}
	}
	if total == 0 {
		return ""
	}
	switch {
	case marked == total:
		return "[x]"
	case marked > 0:
		return "[~]"
	default:
		return "[ ]"
	}
}

// splitToggle flips the selection at the cursor: on a file header it toggles
// all addition/deletion lines in that file; on an addition/deletion line it
// toggles that single line.
func (m *Model) splitToggle() {
	if len(m.diffChunks) == 0 || m.diffCurChunk < 0 || m.diffCurChunk >= len(m.diffChunks) {
		return
	}
	cur := m.diffChunks[m.diffCurChunk]
	if m.diffCurLine < 0 || m.diffCurLine >= len(cur) {
		return
	}
	headLen := m.diffHeadLen()
	rowIdx := cur[m.diffCurLine] - headLen
	if rowIdx < 0 || rowIdx >= len(m.diffRows) {
		return
	}

	r := m.diffRows[rowIdx]
	if m.splitMarked == nil {
		m.splitMarked = map[int]bool{}
	}

	if r.kind == rowFileHeader {
		end := len(m.diffRows)
		for j := rowIdx + 1; j < len(m.diffRows); j++ {
			if m.diffRows[j].kind == rowFileHeader {
				end = j
				break
			}
		}
		allMarked := true
		for j := rowIdx + 1; j < end; j++ {
			if m.diffRows[j].kind == rowLine && (m.diffRows[j].lineKind == "addition" || m.diffRows[j].lineKind == "deletion") {
				if !m.splitMarked[j] {
					allMarked = false
					break
				}
			}
		}
		for j := rowIdx + 1; j < end; j++ {
			if m.diffRows[j].kind == rowLine && (m.diffRows[j].lineKind == "addition" || m.diffRows[j].lineKind == "deletion") {
				m.splitMarked[j] = !allMarked
			}
		}
	} else if r.kind == rowLine && (r.lineKind == "addition" || r.lineKind == "deletion") {
		m.splitMarked[rowIdx] = !m.splitMarked[rowIdx]
	}
}

// splitFileSelection classifies a file's selection state for the confirmation
// logic.
type splitFileSelection struct {
	path          string
	prevPath      string
	changeType    string
	fullyMarked   bool
	fullyUnmarked bool
	partial       bool
}

// classifySplitFiles scans the diff rows and marked set, returning per-file
// selection state.
func classifySplitFiles(rows []diffRow, marked map[int]bool) []splitFileSelection {
	var files []splitFileSelection
	for i, r := range rows {
		if r.kind != rowFileHeader {
			continue
		}
		end := len(rows)
		for j := i + 1; j < len(rows); j++ {
			if rows[j].kind == rowFileHeader {
				end = j
				break
			}
		}
		markedCount, total := 0, 0
		for j := i + 1; j < end; j++ {
			if rows[j].kind == rowLine && (rows[j].lineKind == "addition" || rows[j].lineKind == "deletion") {
				total++
				if marked[j] {
					markedCount++
				}
			}
		}
		if total == 0 {
			continue
		}
		fs := splitFileSelection{
			path:       r.path,
			prevPath:   r.prevPath,
			changeType: r.changeType,
		}
		switch {
		case markedCount == total:
			fs.fullyMarked = true
		case markedCount == 0:
			fs.fullyUnmarked = true
		default:
			fs.partial = true
		}
		files = append(files, fs)
	}
	return files
}

// execSplit is the confirmation handler for split mode. It classifies file
// selections and dispatches to either SplitPaths (file-level) or
// SplitInteractive (line-level). Both paths return splitFinishedMsg so the
// Update handler can auto-route to the newly created revision.
func (m Model) execSplit() (tea.Model, tea.Cmd) {
	if !m.diffIsRevision || m.diffRev == "" || len(m.diffRows) == 0 {
		m.splitMode = false
		m.splitMarked = nil
		return m, nil
	}

	files := classifySplitFiles(m.diffRows, m.splitMarked)

	hasPartial := false
	var unmarkedPaths []string
	for _, f := range files {
		if f.partial {
			hasPartial = true
		}
		if f.fullyUnmarked {
			unmarkedPaths = append(unmarkedPaths, f.path)
		}
	}

	if len(unmarkedPaths) == 0 && !hasPartial {
		m.errMsg = "nothing to split: all changes are marked to keep"
		return m, nil
	}

	rev := m.diffRev
	rows := m.diffRows
	marked := m.splitMarked
	m.splitMode = false
	m.splitMarked = nil

	m, tick := m.startBusy("splitting…")
	if !hasPartial {
		return m, tea.Batch(tick, m.splitPathsCmd(rev, unmarkedPaths))
	}
	return m, tea.Batch(tick, m.splitInteractiveCmd(rev, rows, marked))
}

// splitPathsCmd runs `jj split` with the given file paths and returns a
// splitFinishedMsg carrying the newly created revision's change ID. Extra
// flags are appended for elevation retries.
func (m Model) splitPathsCmd(rev string, paths []string, extra ...string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		selected, err := r.SplitPaths(rev, paths, extra...)
		if err != nil {
			if len(extra) == 0 {
				if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
					return splitFinishedMsg{rev: rev, err: err, elev: &elevReq{
						flag:   flag,
						reason: reason,
						retry:  func() tea.Cmd { return m.splitPathsCmd(rev, paths, flag) },
					}}
				}
			}
			return splitFinishedMsg{rev: rev, err: err}
		}
		return splitFinishedMsg{rev: rev, selectedRev: selected}
	}
}

// splitInteractiveCmd builds the intermediate file versions and runs
// `jj split --interactive --tool gojo-split`. The tool script copies the
// parent tree to $OUTPUT and overwrites files with our pre-computed
// intermediate versions; jj then derives the preceding revision from the
// diff between $LEFT (parent) and $OUTPUT, and the remaining changes stay
// in the original revision. Extra flags are appended for elevation retries.
func (m Model) splitInteractiveCmd(rev string, rows []diffRow, marked map[int]bool, extra ...string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		parentRev, err := r.ParentCommit(rev)
		if err != nil {
			return splitFinishedMsg{rev: rev, err: err}
		}

		tmpDir, err := os.MkdirTemp("", "gojo-split-*")
		if err != nil {
			return splitFinishedMsg{rev: rev, err: err}
		}
		defer os.RemoveAll(tmpDir)

		intermediateDir := filepath.Join(tmpDir, "intermediate")
		if err := os.MkdirAll(intermediateDir, 0755); err != nil {
			return splitFinishedMsg{rev: rev, err: err}
		}

		var oldPaths []string

		for i, row := range rows {
			if row.kind != rowFileHeader {
				continue
			}
			end := len(rows)
			for j := i + 1; j < len(rows); j++ {
				if rows[j].kind == rowFileHeader {
					end = j
					break
				}
			}
			markedCount, total := 0, 0
			for j := i + 1; j < end; j++ {
				if rows[j].kind == rowLine && (rows[j].lineKind == "addition" || rows[j].lineKind == "deletion") {
					total++
					if marked[j] {
						markedCount++
					}
				}
			}
			if total == 0 {
				continue
			}

			path := row.path

			switch {
			case markedCount == total:
				current, ferr := r.FileShow(rev, path)
				if ferr != nil {
					return splitFinishedMsg{rev: rev, err: fmt.Errorf("show %s: %w", path, ferr)}
				}
				if err := writeIntermediateFile(intermediateDir, path, current); err != nil {
					return splitFinishedMsg{rev: rev, err: err}
				}
			case markedCount == 0:
				if row.prevPath != "" && row.prevPath != path {
					oldPaths = append(oldPaths, row.prevPath)
				}
			default:
				parent, perr := r.FileShow(parentRev, path)
				if perr != nil {
					parent = ""
				}
				intermediate := computeIntermediateFile(parent, rows, i, end, marked)
				if err := writeIntermediateFile(intermediateDir, path, intermediate); err != nil {
					return splitFinishedMsg{rev: rev, err: err}
				}
				if row.prevPath != "" && row.prevPath != path {
					oldPaths = append(oldPaths, row.prevPath)
				}
			}
		}

		toolPath := filepath.Join(tmpDir, "gojo-split-tool.sh")
		if err := os.WriteFile(toolPath, []byte(splitToolScript), 0755); err != nil {
			return splitFinishedMsg{rev: rev, err: err}
		}

		selected, err := r.SplitInteractive(rev, toolPath, intermediateDir, oldPaths, extra...)
		if err != nil {
			if len(extra) == 0 {
				if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
					return splitFinishedMsg{rev: rev, err: err, elev: &elevReq{
						flag:   flag,
						reason: reason,
						retry:  func() tea.Cmd { return m.splitInteractiveCmd(rev, rows, marked, flag) },
					}}
				}
			}
			return splitFinishedMsg{rev: rev, err: err}
		}

		return splitFinishedMsg{rev: rev, selectedRev: selected}
	}
}

// writeIntermediateFile writes content to intermediateDir/path, creating parent
// directories as needed.
func writeIntermediateFile(intermediateDir, path, content string) error {
	full := filepath.Join(intermediateDir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0644)
}

// splitToolScript is the shell script invoked by jj as the diff-editor tool.
// It copies the parent tree ($LEFT) to $OUTPUT, removes any old paths (for
// renamed files), then overwrites with the pre-computed intermediate versions.
const splitToolScript = `#!/bin/sh
set -e
LEFT="$1"
RIGHT="$2"
OUTPUT="$3"
mkdir -p "$OUTPUT"
cp -r "$LEFT"/. "$OUTPUT"/
if [ -n "$GOJO_OLD_PATHS" ]; then
  printf '%s\n' "$GOJO_OLD_PATHS" | while IFS= read -r p; do
    [ -n "$p" ] && rm -rf "$OUTPUT/$p"
  done
fi
if [ -n "$GOJO_INTERMEDIATE" ] && [ -d "$GOJO_INTERMEDIATE" ]; then
  cp -rf "$GOJO_INTERMEDIATE"/. "$OUTPUT"/
fi
`

// hunkOldStartRe extracts the old-side starting line number from a hunk header.
var hunkOldStartRe = regexp.MustCompile(`@@ -(\d+)`)

// parseHunkOldStart returns the 1-based old-side start line from a hunk header
// string like "@@ -10,5 +12,5 @@".
func parseHunkOldStart(hunkText string) int {
	m := hunkOldStartRe.FindStringSubmatch(hunkText)
	if m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 1
}

// computeIntermediateFile reconstructs the file content for the preceding
// revision: parent content with only the unmarked changes applied. Marked
// changes (those the user wants to keep in the current revision) are not
// applied.
//
// The algorithm walks the diff rows for a single file (rows[startIdx:endIdx]),
// tracking position in the parent file. For each line:
//   - Context: output as-is, advance parent position.
//   - Deletion: if unmarked, skip (removed in preceding); if marked, output
//     the old text (deletion stays in current, so the line survives in
//     preceding). Advance parent position either way.
//   - Addition: if unmarked, output (added in preceding); if marked, skip
//     (addition stays in current). Don't advance parent position.
func computeIntermediateFile(parentContent string, rows []diffRow, startIdx, endIdx int, marked map[int]bool) string {
	var parentLines []string
	if parentContent != "" {
		parentLines = strings.Split(parentContent, "\n")
		if len(parentLines) > 0 && parentLines[len(parentLines)-1] == "" {
			parentLines = parentLines[:len(parentLines)-1]
		}
	}

	var out strings.Builder
	p := 0 // 0-indexed position in parentLines

	for i := startIdx; i < endIdx; i++ {
		r := rows[i]
		if r.kind == rowHunkHeader {
			oldStart := parseHunkOldStart(r.hunkText)
			target := oldStart - 1 // 0-indexed
			for p < target && p < len(parentLines) {
				out.WriteString(parentLines[p])
				out.WriteString("\n")
				p++
			}
			continue
		}
		if r.kind != rowLine {
			continue
		}

		text := spansText(r.spans)

		switch r.lineKind {
		case "context":
			if r.oldNum > 0 {
				target := r.oldNum - 1
				for p < target && p < len(parentLines) {
					out.WriteString(parentLines[p])
					out.WriteString("\n")
					p++
				}
			}
			out.WriteString(text)
			out.WriteString("\n")
			if r.oldNum > 0 {
				p = r.oldNum
			}

		case "deletion":
			if r.oldNum > 0 {
				target := r.oldNum - 1
				for p < target && p < len(parentLines) {
					out.WriteString(parentLines[p])
					out.WriteString("\n")
					p++
				}
			}
			if marked[i] {
				out.WriteString(text)
				out.WriteString("\n")
			}
			if r.oldNum > 0 {
				p = r.oldNum
			}

		case "addition":
			if !marked[i] {
				out.WriteString(text)
				out.WriteString("\n")
			}
		}
	}

	for p < len(parentLines) {
		out.WriteString(parentLines[p])
		out.WriteString("\n")
		p++
	}

	return out.String()
}
