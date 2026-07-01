package ui

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// span is a styled run of text within a rendered diff line.
type span struct {
	text string
	fg   string // hex color, empty = inherit line color
	bg   string // hex color, empty = inherit line background
}

type rowKind int

const (
	rowFileHeader rowKind = iota
	rowHunkHeader
	rowLine
)

// diffRow is one rendered row: a file header, hunk header, or content line.
type diffRow struct {
	kind rowKind

	// file header
	path       string
	prevPath   string
	changeType string

	// hunk header
	hunkText string

	// content line
	lineKind string // "addition" | "deletion" | "context"
	sign     string
	oldNum   int // 0 = none
	newNum   int // 0 = none
	spans    []span
}

// ── git diff parsing ───────────────────────────────────────────────────────

type pContent struct {
	isHunk   bool
	hunkText string
	kind     string
	sign     string
	oldNum   int
	newNum   int
	side     int // 0 = new side, 1 = old side
	idx      int
	text     string
}

type pFile struct {
	fromPath  string
	toPath    string
	isNew     bool
	isDeleted bool
	isRename  bool
	contents  []pContent
	newSide   []string
	oldSide   []string
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// renderDiff parses raw git-format unified diff text into styled rows,
// syntax-highlighting line content via chroma.
func renderDiff(raw string) []diffRow {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var files []*pFile
	var cur *pFile
	inHunk := false
	delNum, addNum := 0, 0

	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur = &pFile{}
			files = append(files, cur)
			inHunk = false
			rest := line[len("diff --git "):]
			if i := strings.Index(rest, " b/"); i >= 0 {
				cur.fromPath = strings.TrimPrefix(rest[:i], "a/")
				cur.toPath = strings.TrimPrefix(rest[i+1:], "b/")
			}
			continue
		}

		if cur == nil {
			continue
		}

		if !inHunk {
			switch {
			case strings.HasPrefix(line, "new file mode"):
				cur.isNew = true
				continue
			case strings.HasPrefix(line, "deleted file mode"):
				cur.isDeleted = true
				continue
			case strings.HasPrefix(line, "rename from "):
				cur.isRename = true
				cur.fromPath = line[len("rename from "):]
				continue
			case strings.HasPrefix(line, "rename to "):
				cur.isRename = true
				cur.toPath = line[len("rename to "):]
				continue
			case strings.HasPrefix(line, "copy from "):
				cur.isRename = true
				cur.fromPath = line[len("copy from "):]
				continue
			case strings.HasPrefix(line, "copy to "):
				cur.isRename = true
				cur.toPath = line[len("copy to "):]
				continue
			case strings.HasPrefix(line, "--- "):
				p := line[4:]
				if p == "/dev/null" {
					cur.isNew = true
				} else {
					cur.fromPath = strings.TrimPrefix(p, "a/")
				}
				continue
			case strings.HasPrefix(line, "+++ "):
				p := line[4:]
				if p == "/dev/null" {
					cur.isDeleted = true
				} else {
					cur.toPath = strings.TrimPrefix(p, "b/")
				}
				continue
			case !strings.HasPrefix(line, "@@"):
				// index / mode / similarity lines — ignore
				continue
			}
		}

		if m := hunkRe.FindStringSubmatch(line); m != nil {
			inHunk = true
			delNum = atoi(m[1])
			addNum = atoi(m[3])
			delCount, addCount := 1, 1
			if m[2] != "" {
				delCount = atoi(m[2])
			}
			if m[4] != "" {
				addCount = atoi(m[4])
			}
			cur.contents = append(cur.contents, pContent{
				isHunk:   true,
				hunkText: "@@ -" + strconv.Itoa(delNum) + "," + strconv.Itoa(delCount) + " +" + strconv.Itoa(addNum) + "," + strconv.Itoa(addCount) + " @@",
			})
			continue
		}

		if !inHunk {
			continue
		}

		// Content line.
		if strings.HasPrefix(line, "\\") {
			// "\ No newline at end of file" — ignore
			continue
		}
		var prefix byte = ' '
		body := ""
		if line != "" {
			prefix = line[0]
			body = line[1:]
		}

		switch prefix {
		case '+':
			idx := len(cur.newSide)
			cur.newSide = append(cur.newSide, body)
			cur.contents = append(cur.contents, pContent{
				kind: "addition", sign: "+", newNum: addNum, side: 0, idx: idx, text: body,
			})
			addNum++
		case '-':
			idx := len(cur.oldSide)
			cur.oldSide = append(cur.oldSide, body)
			cur.contents = append(cur.contents, pContent{
				kind: "deletion", sign: "-", oldNum: delNum, side: 1, idx: idx, text: body,
			})
			delNum++
		default:
			idx := len(cur.newSide)
			cur.newSide = append(cur.newSide, body)
			cur.oldSide = append(cur.oldSide, body)
			cur.contents = append(cur.contents, pContent{
				kind: "context", sign: " ", oldNum: delNum, newNum: addNum, side: 0, idx: idx, text: body,
			})
			delNum++
			addNum++
		}
	}

	var rows []diffRow
	for _, f := range files {
		filename := f.toPath
		if filename == "" {
			filename = f.fromPath
		}
		newSpans := highlightLines(filename, f.newSide)
		oldSpans := highlightLines(filename, f.oldSide)

		changeType := "modified"
		path := f.toPath
		prevPath := ""
		switch {
		case f.isNew:
			changeType = "added"
			path = f.toPath
		case f.isDeleted:
			changeType = "deleted"
			path = f.fromPath
		case f.isRename:
			changeType = "renamed"
			path = f.toPath
			prevPath = f.fromPath
		}

		rows = append(rows, diffRow{
			kind:       rowFileHeader,
			path:       path,
			prevPath:   prevPath,
			changeType: changeType,
		})

		for _, c := range f.contents {
			if c.isHunk {
				rows = append(rows, diffRow{kind: rowHunkHeader, hunkText: c.hunkText})
				continue
			}
			var sp []span
			if c.side == 0 && newSpans != nil && c.idx < len(newSpans) {
				sp = newSpans[c.idx]
			} else if c.side == 1 && oldSpans != nil && c.idx < len(oldSpans) {
				sp = oldSpans[c.idx]
			}
			if sp == nil {
				if c.text != "" {
					sp = []span{{text: c.text}}
				} else {
					sp = []span{}
				}
			}
			rows = append(rows, diffRow{
				kind:     rowLine,
				lineKind: c.kind,
				sign:     c.sign,
				oldNum:   c.oldNum,
				newNum:   c.newNum,
				spans:    sp,
			})
		}
	}
	applyWordDiffToRows(rows)
	return rows
}

// ── chroma syntax highlighting ─────────────────────────────────────────────

// chromaStyleOnce caches a chroma style chosen to match the terminal
// background (light vs dark), resolved on first diff render.
var (
	chromaStyleOnce sync.Once
	chromaStyleVal  *chroma.Style
)

func chromaStyle() *chroma.Style {
	chromaStyleOnce.Do(func() {
		name := "github-dark"
		if !lipgloss.HasDarkBackground() {
			name = "github"
		}
		if s := styles.Get(name); s != nil {
			chromaStyleVal = s
		} else {
			chromaStyleVal = styles.Fallback
		}
	})
	return chromaStyleVal
}

// highlightLines syntax-highlights each source line, returning per-line spans.
// Returns nil when no lexer matches (caller falls back to plain text).
func highlightLines(filename string, lines []string) [][]span {
	if len(lines) == 0 {
		return [][]span{}
	}
	lexer := lexers.Match(filename)
	if lexer == nil {
		return nil
	}
	lexer = chroma.Coalesce(lexer)

	source := strings.Join(lines, "\n")
	it, err := lexer.Tokenise(nil, source)
	if err != nil {
		return nil
	}
	perLine := chroma.SplitTokensIntoLines(it.Tokens())
	if len(perLine) > len(lines) {
		return nil // unexpected misalignment — fall back to plain
	}

	out := make([][]span, len(lines))
	for i := range out {
		if i >= len(perLine) {
			out[i] = []span{} // trailing empty source line dropped by splitter
			continue
		}
		var spans []span
		for _, t := range perLine[i] {
			text := strings.TrimRight(t.Value, "\n")
			if text == "" {
				continue
			}
			fg := ""
			if c := chromaStyle().Get(t.Type).Colour; c.IsSet() {
				fg = c.String()
			}
			if n := len(spans); n > 0 && spans[n-1].fg == fg {
				spans[n-1].text += text
			} else {
				spans = append(spans, span{text: text, fg: fg})
			}
		}
		out[i] = spans
	}
	return out
}

// ── word-level diff highlighting ────────────────────────────────────────────

// wordToken is a whitespace-delimited token used for word-level diffing.
type wordToken struct {
	text  string
	start int // byte offset in the original line text
}

// tokenizeWords splits a string into alternating word and whitespace tokens.
func tokenizeWords(s string) []wordToken {
	var tokens []wordToken
	i := 0
	for i < len(s) {
		if s[i] == ' ' || s[i] == '\t' {
			j := i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			tokens = append(tokens, wordToken{text: s[i:j], start: i})
			i = j
		} else {
			j := i
			for j < len(s) && s[j] != ' ' && s[j] != '\t' {
				j++
			}
			tokens = append(tokens, wordToken{text: s[i:j], start: i})
			i = j
		}
	}
	return tokens
}

// wordDiffClass classifies a word token in a word-level diff.
type wordDiffClass int

const (
	wordCommon wordDiffClass = iota // token is in both old and new
	wordAdded                       // token is only in new
	wordRemoved                     // token is only in old
)

// computeWordDiff computes a word-level diff between old and new text using
// longest common subsequence (LCS) on word tokens. Returns per-token
// classifications for old and new.
func computeWordDiff(oldText, newText string) ([]wordDiffClass, []wordDiffClass) {
	oldTokens := tokenizeWords(oldText)
	newTokens := tokenizeWords(newText)
	m, n := len(oldTokens), len(newTokens)

	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldTokens[i-1].text == newTokens[j-1].text {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	oldClass := make([]wordDiffClass, m)
	newClass := make([]wordDiffClass, n)
	for i := range oldClass {
		oldClass[i] = wordRemoved
	}
	for i := range newClass {
		newClass[i] = wordAdded
	}

	i, j := m, n
	for i > 0 && j > 0 {
		if oldTokens[i-1].text == newTokens[j-1].text {
			oldClass[i-1] = wordCommon
			newClass[j-1] = wordCommon
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	return oldClass, newClass
}

// highlightRanges extracts merged byte ranges that should be highlighted from
// the word diff classification. Adjacent highlighted tokens are merged into a
// single range.
func highlightRanges(tokens []wordToken, class []wordDiffClass) [][2]int {
	var ranges [][2]int
	for i, c := range class {
		if c == wordCommon {
			continue
		}
		start := tokens[i].start
		end := start + len(tokens[i].text)
		if len(ranges) > 0 && ranges[len(ranges)-1][1] == start {
			ranges[len(ranges)-1][1] = end
		} else {
			ranges = append(ranges, [2]int{start, end})
		}
	}
	return ranges
}

// splitSpansByRanges splits existing spans at the given byte ranges, marking
// portions within ranges with the given bg color. Ranges must be sorted and
// non-overlapping. The span's original fg is preserved.
func splitSpansByRanges(spans []span, ranges [][2]int, bg string) []span {
	if len(ranges) == 0 || len(spans) == 0 {
		return spans
	}

	var result []span
	linePos := 0 // byte offset within the line
	ri := 0      // range index

	for _, s := range spans {
		sStart := linePos
		sEnd := linePos + len(s.text)

		for ri < len(ranges) && ranges[ri][1] <= sStart {
			ri++
		}

		cursor := sStart
		for ri < len(ranges) && ranges[ri][0] < sEnd {
			r := ranges[ri]

			if r[0] > cursor {
				result = append(result, span{text: s.text[cursor-sStart : r[0]-sStart], fg: s.fg})
			}

			rEnd := r[1]
			if rEnd > sEnd {
				rEnd = sEnd
			}
			relStart := max(r[0], cursor) - sStart
			relEnd := rEnd - sStart
			result = append(result, span{text: s.text[relStart:relEnd], fg: s.fg, bg: bg})
			cursor = rEnd

			if r[1] <= sEnd {
				ri++
			} else {
				break
			}
		}

		if cursor < sEnd {
			result = append(result, span{text: s.text[cursor-sStart:], fg: s.fg})
		}

		linePos = sEnd
	}

	return result
}

// spansText returns the plain text content of a slice of spans.
func spansText(spans []span) string {
	if len(spans) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.text)
	}
	return sb.String()
}

var (
	diffWordBgOnce    sync.Once
	diffAddedWordBg   string
	diffRemovedWordBg string
)

// diffWordBgColors returns the hex background colors for word-level added and
// removed token highlights, adapted to the terminal's background.
func diffWordBgColors() (added, removed string) {
	diffWordBgOnce.Do(func() {
		if lipgloss.HasDarkBackground() {
			diffAddedWordBg = "#2d5a3d"
			diffRemovedWordBg = "#5a2d3d"
		} else {
			diffAddedWordBg = "#b8e6c8"
			diffRemovedWordBg = "#e8c8cc"
		}
	})
	return diffAddedWordBg, diffRemovedWordBg
}

// maxWordDiffLen is the maximum line length for which word-level diff is
// computed. Longer lines fall back to line-level highlighting only.
const maxWordDiffLen = 2000

// applyWordDiffToRows identifies modified runs (consecutive deletion rows
// followed by consecutive addition rows within the same hunk) and applies
// word-level diff highlighting to the changed tokens. Pure additions or pure
// deletions are left untouched.
func applyWordDiffToRows(rows []diffRow) {
	addedBg, removedBg := diffWordBgColors()
	i := 0
	for i < len(rows) {
		if rows[i].kind != rowLine || rows[i].lineKind != "deletion" {
			i++
			continue
		}

		delStart := i
		for i < len(rows) && rows[i].kind == rowLine && rows[i].lineKind == "deletion" {
			i++
		}
		delEnd := i

		addStart := i
		for i < len(rows) && rows[i].kind == rowLine && rows[i].lineKind == "addition" {
			i++
		}
		addEnd := i

		delCount := delEnd - delStart
		addCount := addEnd - addStart

		if delCount == 0 || addCount == 0 {
			continue
		}

		pairs := min(delCount, addCount)
		for k := 0; k < pairs; k++ {
			delRow := &rows[delStart+k]
			addRow := &rows[addStart+k]

			oldText := spansText(delRow.spans)
			newText := spansText(addRow.spans)

			if len(oldText) > maxWordDiffLen || len(newText) > maxWordDiffLen {
				continue
			}

			oldClass, newClass := computeWordDiff(oldText, newText)
			oldTokens := tokenizeWords(oldText)
			newTokens := tokenizeWords(newText)

			oldRanges := highlightRanges(oldTokens, oldClass)
			newRanges := highlightRanges(newTokens, newClass)

			delRow.spans = splitSpansByRanges(delRow.spans, oldRanges, removedBg)
			addRow.spans = splitSpansByRanges(addRow.spans, newRanges, addedBg)
		}
	}
}
