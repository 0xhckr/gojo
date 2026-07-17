package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// searchResult is one matched revision from the fzf-style search. The per-field
// matched masks are rune-indexed into the corresponding LogEntry field string;
// nil means the query did not match that field.
type searchResult struct {
	entryIdx  int
	score     int
	changeID  []bool
	commitID  []bool
	subject   []bool
	author    []bool
	bookmarks []bool // matched against space-joined bookmarks
	tags      []bool // matched against space-joined tags
}

// matchEntry fuzzy-matches query against an entry's searchable fields: change
// ID, commit ID, description (subject), author email, bookmark names, and git
// tags. The best score across all matched fields is used for ranking.
func matchEntry(query string, e jj.LogEntry) (searchResult, bool) {
	if query == "" {
		return searchResult{}, true
	}
	var best searchResult
	best.score = -1

	take := func(r fzfResult, mask *[]bool) {
		*mask = r.matched
		if r.score > best.score {
			best.score = r.score
		}
	}

	if r, ok := fuzzyMatch(query, e.ChangeID); ok {
		best.score = r.score
		best.changeID = r.matched
	}
	if r, ok := fuzzyMatch(query, e.CommitID); ok {
		take(r, &best.commitID)
	}
	if r, ok := fuzzyMatch(query, e.Subject); ok {
		take(r, &best.subject)
	}
	if r, ok := fuzzyMatch(query, e.Authors); ok {
		take(r, &best.author)
	}
	if bm := strings.Join(e.Bookmarks, " "); bm != "" {
		if r, ok := fuzzyMatch(query, bm); ok {
			take(r, &best.bookmarks)
		}
	}
	if tg := strings.Join(e.Tags, " "); tg != "" {
		if r, ok := fuzzyMatch(query, tg); ok {
			take(r, &best.tags)
		}
	}

	if best.score < 0 {
		return searchResult{}, false
	}
	return best, true
}

// searchFilter re-computes the fuzzy match results for the current query,
// sorted by score (descending) then by original log order. The cursor is
// clamped into the new result set.
func (m *Model) searchFilter() {
	var results []searchResult
	for i, e := range m.entries {
		r, ok := matchEntry(m.searchQuery, e)
		if ok {
			r.entryIdx = i
			results = append(results, r)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].entryIdx < results[j].entryIdx
	})
	m.searchResults = results
	if m.searchCursor >= len(results) {
		m.searchCursor = max(0, len(results)-1)
	}
	if m.searchOffset > m.searchCursor {
		m.searchOffset = m.searchCursor
	}
}

// highlightMatched splits s into styled segments, colouring matched runes (per
// the matched mask) with hl and the rest with base. A nil mask produces a
// single base-coloured segment.
func highlightMatched(s string, matched []bool, base, hl, bg lipgloss.TerminalColor) []seg {
	if s == "" {
		return nil
	}
	if matched == nil {
		return []seg{{text: s, fg: base, bg: bg}}
	}
	sr := []rune(s)
	var segs []seg
	var buf strings.Builder
	bufMatched := false
	for si, ch := range sr {
		isMatch := si < len(matched) && matched[si]
		if si == 0 {
			bufMatched = isMatch
		}
		if isMatch != bufMatched {
			if buf.Len() > 0 {
				fg := base
				if bufMatched {
					fg = hl
				}
				segs = append(segs, seg{text: buf.String(), fg: fg, bold: bufMatched, bg: bg})
				buf.Reset()
			}
			bufMatched = isMatch
		}
		buf.WriteRune(ch)
	}
	if buf.Len() > 0 {
		fg := base
		if bufMatched {
			fg = hl
		}
		segs = append(segs, seg{text: buf.String(), fg: fg, bold: bufMatched, bg: bg})
	}
	return segs
}

// searchVisibleRange computes the [start, end) row window for the search
// cursor, updating the offset for scroll tracking.
func searchVisibleRange(cursor, offset, total, height int) (int, int) {
	off := offset
	if cursor < off {
		off = cursor
	}
	end := off
	used := 0
	for end < total && used < height {
		used++
		end++
	}
	if cursor >= end {
		off = max(0, cursor-height+1)
		end = cursor + 1
	}
	return off, end
}

// renderSearch renders the fzf-style search overlay: a title bar, a prompt bar
// with the live query and match count, a divider, then the filtered result
// list. Each result is a compact one-line row showing change ID, author,
// subject, and any bookmarks/tags, with matched characters highlighted in
// yellow. A scrollbar tracks position when results overflow.
func (m Model) renderSearch(width, height int) []string {
	// Title bar.
	titleLeft := " search revisions"
	titleRight := " esc cancel · ⏎ jump · type to filter "
	pad := max(1, width-len(titleLeft)-len(titleRight))
	out := []string{bgRow(width, colDarkPurple,
		seg{text: titleLeft, fg: colPurple, bg: colDarkPurple},
		seg{text: strings.Repeat(" ", pad), bg: colDarkPurple},
		seg{text: titleRight, fg: colGray, bg: colDarkPurple},
	)}

	// Prompt bar — ┃ /  query█  …  N matches
	queryStr := m.searchQuery + "█"
	var matchStr string
	if len(m.searchResults) == 0 {
		matchStr = "no matches"
	} else {
		matchStr = fmt.Sprintf("%d matches", len(m.searchResults))
	}
	leftW := lipgloss.Width("┃ /  ") + lipgloss.Width(queryStr)
	rightW := lipgloss.Width(matchStr) + 1
	promptPad := max(0, width-leftW-rightW)
	prompt := bgRow(width, colPanel,
		seg{text: "┃ ", fg: colCyan, bold: true, bg: colPanel},
		seg{text: "/  ", fg: colTextMuted, bg: colPanel},
		seg{text: queryStr, fg: colYellow, bold: true, bg: colPanel},
		seg{text: strings.Repeat(" ", promptPad), bg: colPanel},
		seg{text: matchStr, fg: colTextMuted, bg: colPanel},
		seg{text: " ", bg: colPanel},
	)
	out = append(out, prompt)

	// Divider.
	out = append(out, bgRow(width, colPanel, seg{text: strings.Repeat("─", width), fg: colBorder, bg: colPanel}))

	// Results.
	contentH := height - 3 // title + prompt + divider
	if contentH < 0 {
		contentH = 0
	}

	if len(m.searchResults) == 0 || contentH == 0 {
		if contentH > 0 {
			out = append(out, bgRow(width, colPanel, seg{text: "  (no matches)", fg: colTextMuted, bg: colPanel}))
		}
		return padLines(out, height, width)
	}

	// Scrollbar.
	total := len(m.searchResults)
	scrollW := width
	visLines := min(total, contentH)
	thumbStart, thumbEnd := scrollbarThumb(total, m.searchOffset, visLines, contentH)
	hasBar := thumbStart >= 0
	if hasBar {
		scrollW -= scrollbarWidth
	}

	start, end := searchVisibleRange(m.searchCursor, m.searchOffset, total, contentH)
	m.searchOffset = start

	contentLine := 0
	for i := start; i < end; i++ {
		r := m.searchResults[i]
		e := m.entries[r.entryIdx]
		selected := i == m.searchCursor
		hovered := m.hover.searchRow == i && !selected
		bg := colPanel
		if selected {
			bg = colElement
		} else if hovered {
			bg = colHover
		}
		barFg := bg
		if selected {
			barFg = colYellow
		}

		var segs []seg
		segs = append(segs, seg{text: "┃", fg: barFg, bold: true, bg: bg})
		segs = append(segs, seg{text: " ", bg: bg})

		// Change ID.
		segs = append(segs, highlightMatched(e.ChangeID, r.changeID, colMagenta, colYellow, bg)...)
		segs = append(segs, seg{text: " ", bg: bg})

		// Author.
		segs = append(segs, highlightMatched(e.Authors, r.author, colBlue, colYellow, bg)...)
		segs = append(segs, seg{text: " ", bg: bg})

		// Subject.
		subject := e.Subject
		if subject == "" {
			subject = "(no description set)"
		}
		subjectFg := colText
		if e.IsWorkingCopy {
			subjectFg = colYellow
		} else if e.IsImmutable {
			subjectFg = colTextMuted
		}
		segs = append(segs, highlightMatched(subject, r.subject, subjectFg, colYellow, bg)...)

		// Bookmarks.
		if bm := strings.Join(e.Bookmarks, " "); bm != "" {
			segs = append(segs, seg{text: " ", bg: bg})
			segs = append(segs, highlightMatched(bm, r.bookmarks, colGreen, colYellow, bg)...)
		}

		// Tags.
		if tg := strings.Join(e.Tags, " "); tg != "" {
			segs = append(segs, seg{text: " ", bg: bg})
			segs = append(segs, highlightMatched(tg, r.tags, colTeal, colYellow, bg)...)
		}

		out = append(out, renderRowWithBar(scrollW, width, bg, hasBar, contentLine, thumbStart, thumbEnd, segs))
		contentLine++
	}

	return padLines(out, height, width)
}
