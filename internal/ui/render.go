package ui

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// seg is a styled run of text used to compose a single terminal line.
// A nil fg or bg means "use the terminal default".
type seg struct {
	text      string
	fg, bg    lipgloss.TerminalColor
	bold      bool
	underline bool
	faint     bool
}

// ── Style cache ─────────────────────────────────────────────────────────────
//
// Building a fresh lipgloss.Style per segment and calling Render was the
// dominant per-frame cost: Render resolves the color profile, rebuilds the
// SGR sequence, and walks the text on every call. The set of distinct styles
// the UI uses is tiny and the terminal profile is fixed for the process
// lifetime, so the rendered byte shape of a style is a pure function of the
// style attributes. We probe it once per unique combination (validating the
// fast path against real Render output) and reuse the escape sequences.

type styleKey struct {
	fg, bg    lipgloss.TerminalColor
	bold      bool
	underline bool
	faint     bool
	profile   termenv.Profile
	dark      bool
}

type renderMode uint8

const (
	modePassthrough renderMode = iota // style renders text unchanged
	modeBulk                          // prefix + text + suffix
	modePerRune                       // prefix + rune + suffix, per rune (space styler)
	modeLipgloss                      // unexpected shape: defer to lipgloss Render
)

type cachedStyle struct {
	mode           renderMode
	prefix, suffix string
	style          lipgloss.Style // modeLipgloss fallback
}

var (
	// styleCacheAtomic holds an immutable map[styleKey]cachedStyle snapshot.
	// Reads are a plain atomic load + map access (no mutex); the rare miss
	// path takes styleMu, fills, and publishes a fresh snapshot. The set of
	// styles in use is small and fixed after warm-up.
	styleCacheAtomic atomic.Value
	styleMu          sync.Mutex
)

func styleFor(s seg) cachedStyle {
	r := lipgloss.DefaultRenderer()
	k := styleKey{
		fg: s.fg, bg: s.bg, bold: s.bold, underline: s.underline, faint: s.faint,
		profile: r.ColorProfile(), dark: r.HasDarkBackground(),
	}
	if v := styleCacheAtomic.Load(); v != nil {
		if cs, ok := v.(map[styleKey]cachedStyle)[k]; ok {
			return cs
		}
	}

	styleMu.Lock()
	defer styleMu.Unlock()
	// Re-check under the write lock.
	if v := styleCacheAtomic.Load(); v != nil {
		if cs, ok := v.(map[styleKey]cachedStyle)[k]; ok {
			return cs
		}
	}

	st := lipgloss.NewStyle()
	if k.fg != nil {
		st = st.Foreground(k.fg)
	}
	if k.bg != nil {
		st = st.Background(k.bg)
	}
	if k.bold {
		st = st.Bold(true)
	}
	if k.underline {
		st = st.Underline(true)
	}
	if k.faint {
		st = st.Faint(true)
	}

	cs := probeStyle(st)

	next := map[styleKey]cachedStyle{k: cs}
	if v := styleCacheAtomic.Load(); v != nil {
		for k2, cs2 := range v.(map[styleKey]cachedStyle) {
			next[k2] = cs2
		}
	}
	styleCacheAtomic.Store(next)
	return cs
}

// probeStyle determines the byte shape lipgloss gives a style and validates
// the fast rendering path against real Render output. Any deviation falls
// back to modeLipgloss (always correct).
func probeStyle(st lipgloss.Style) cachedStyle {
	cs := cachedStyle{mode: modeLipgloss, style: st}

	rA := st.Render("A")
	i := strings.IndexByte(rA, 'A')
	if i < 0 {
		return cs
	}
	prefix, suffix := rA[:i], rA[i+1:]

	// Passthrough: no styling bytes at all.
	if prefix == "" && suffix == "" {
		if st.Render("AB") == "AB" && st.Render("A B") == "A B" {
			cs.mode = modePassthrough
		}
		return cs
	}

	wrap := func(s string) string { return prefix + s + suffix }
	perRune := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			b.WriteString(prefix)
			b.WriteString(string(r))
			b.WriteString(suffix)
		}
		return b.String()
	}

	// Note: "" is excluded — lipgloss emits bare escape sequences for an
	// empty string under a colored style (bulk/perRune differ there), but
	// empty segment texts never occur in practice.
	samples := []string{"AB", "A B", "→x y", "  tail  ", "   "}
	bulkOK, perRuneOK := true, true
	for _, s := range samples {
		want := st.Render(s)
		if wrap(s) != want {
			bulkOK = false
		}
		if perRune(s) != want {
			perRuneOK = false
		}
	}
	switch {
	case bulkOK:
		cs.mode, cs.prefix, cs.suffix = modeBulk, prefix, suffix
	case perRuneOK:
		cs.mode, cs.prefix, cs.suffix = modePerRune, prefix, suffix
	}
	return cs
}

// apply writes the text with this style's escape sequences to b.
func (cs cachedStyle) apply(b *strings.Builder, text string) {
	switch cs.mode {
	case modePassthrough:
		b.WriteString(text)
	case modeBulk:
		b.WriteString(cs.prefix)
		b.WriteString(text)
		b.WriteString(cs.suffix)
	case modePerRune:
		for _, r := range text {
			b.WriteString(cs.prefix)
			b.WriteString(string(r))
			b.WriteString(cs.suffix)
		}
	default:
		b.WriteString(cs.style.Render(text))
	}
}

// render returns the styled text as a new string.
func (cs cachedStyle) render(text string) string {
	switch cs.mode {
	case modePassthrough:
		return text
	case modeBulk:
		return cs.prefix + text + cs.suffix
	case modePerRune:
		var b strings.Builder
		cs.apply(&b, text)
		return b.String()
	default:
		return cs.style.Render(text)
	}
}

// segTextWidth is the display cell width of plain segment text (no ANSI).
// Pure-ASCII text (the very common case: IDs, dates, emails, most subjects)
// is just len(s) — the grapheme-cluster-aware x/ansi path is only taken for
// non-ASCII text.
func segTextWidth(s string) int {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x7f || c < 0x20 {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

// bgStyler returns the cached style that carries only a background color —
// used for full-width fills and padding. A nil bg means "no styling".
func bgStyler(bg lipgloss.TerminalColor) cachedStyle {
	return styleFor(seg{bg: bg})
}

// ── Line composition ────────────────────────────────────────────────────────

// renderSegs renders a sequence of styled segments into one ANSI string.
// Each segment carries its own background so resets between segments never
// leave a visible gap in a filled row.
func renderSegs(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		styleFor(s).apply(&b, s.text)
	}
	return b.String()
}

// clip truncates a (possibly styled) line to width columns, preserving ANSI.
func clip(s string, width int) string {
	if width < 0 {
		width = 0
	}
	return ansi.Truncate(s, width, "")
}

// plainRow renders segments and clips to width (no background fill).
func plainRow(width int, segs ...seg) string {
	return clip(renderSegs(segs), width)
}

// bgRow renders segments over a full-width background, padding then clipping.
// Segments without an explicit background inherit bg.
//
// The visible width is accumulated while rendering (the sum of per-segment
// ANSI-aware widths equals the width of the concatenation for all text the UI
// produces), avoiding a second ANSI-stripping scan of the rendered row; the
// clip is skipped entirely when nothing overflows (the common case).
func bgRow(width int, bg lipgloss.TerminalColor, segs ...seg) string {
	for i := range segs {
		if segs[i].bg == nil {
			segs[i].bg = bg
		}
	}
	var b strings.Builder
	w := 0
	for _, s := range segs {
		styleFor(s).apply(&b, s.text)
		if w <= width {
			w += segTextWidth(s.text)
		}
	}
	if w < width {
		bgStyler(bg).apply(&b, strings.Repeat(" ", width-w))
		return b.String()
	}
	if w == width {
		return b.String()
	}
	return clip(b.String(), width)
}

// blankRow returns a width-wide row filled with bg (or empty if bg == nil).
// Rendered rows are cached: padding to full height re-requests the same blank
// row many times per frame.
func blankRow(width int, bg lipgloss.TerminalColor) string {
	if bg == nil || width <= 0 {
		return ""
	}
	r := lipgloss.DefaultRenderer()
	k := blankKey{width: width, bg: bg, profile: r.ColorProfile(), dark: r.HasDarkBackground()}
	blankMu.RLock()
	s, ok := blankCache[k]
	blankMu.RUnlock()
	if ok {
		return s
	}
	s = bgStyler(bg).render(strings.Repeat(" ", width))
	blankMu.Lock()
	blankCache[k] = s
	blankMu.Unlock()
	return s
}

type blankKey struct {
	width   int
	bg      lipgloss.TerminalColor
	profile termenv.Profile
	dark    bool
}

var (
	blankMu    sync.RWMutex
	blankCache = make(map[blankKey]string)
)

// tabStop is how many cells a tab renders as. Raw tabs must never reach the
// terminal: the width libraries count '\t' as 0 cells while terminals expand
// it to the next tab stop, so a tabbed line comes out physically wider than
// computed, soft-wraps mid-frame, and corrupts the alt screen. Go source is
// tab-indented, so every Go diff hits this.
const tabStop = 4

// expandTabs replaces tabs with tabStop spaces. Cheap no-allocation passthrough
// when there are no tabs.
func expandTabs(s string) string {
	if strings.IndexByte(s, '\t') < 0 {
		return s
	}
	return strings.ReplaceAll(s, "\t", "    ")
}

// runeWidth returns the display cell width of a rune (0 for combining marks).
// Tabs count as tabStop so wrap counters stay consistent with expandTabs.
func runeWidth(r rune) int {
	if r == '\t' {
		return tabStop
	}
	return runewidth.RuneWidth(r)
}

// runeWidthStr returns the total display cell width of s.
func runeWidthStr(s string) int {
	w := 0
	for _, r := range s {
		w += runewidth.RuneWidth(r)
	}
	return w
}

// wrapSegs greedily wraps a sequence of styled segments into lines no wider
// than `width` cells, splitting segments mid-text as needed. Styling is
// preserved per emitted rune and adjacent same-style runes are merged back into
// a single segment so the output stays compact. Always returns at least one
// line (possibly empty). Used to render the visible diff rows only.
func wrapSegs(segs []seg, width int) [][]seg {
	if width < 1 {
		width = 1
	}
	var lines [][]seg
	var cur []seg
	// buf accumulates runes of the pending segment; curStyle is its style.
	// Runes are buffered so merged segments pay one string conversion per run
	// instead of one allocation per rune.
	var buf []rune
	var curStyle seg
	w := 0

	// flush pushes the buffered runes into cur, merging with the previous
	// segment when the style matches.
	flush := func() {
		if len(buf) == 0 {
			return
		}
		if n := len(cur); n > 0 {
			last := &cur[n-1]
			if last.fg == curStyle.fg && last.bg == curStyle.bg && last.bold == curStyle.bold && last.underline == curStyle.underline && last.faint == curStyle.faint {
				last.text += string(buf)
				buf = buf[:0]
				return
			}
		}
		cur = append(cur, seg{text: string(buf), fg: curStyle.fg, bg: curStyle.bg, bold: curStyle.bold, underline: curStyle.underline, faint: curStyle.faint})
		buf = buf[:0]
	}

	for _, s := range segs {
		if s.text == "" {
			continue
		}
		if s != curStyle && len(buf) > 0 {
			flush()
		}
		curStyle = s
		for _, r := range s.text {
			rw := runeWidth(r)
			if w+rw > width && w > 0 {
				flush()
				lines = append(lines, cur)
				cur = nil
				w = 0
			}
			buf = append(buf, r)
			w += rw
		}
	}
	flush()
	lines = append(lines, cur) // flush final line (empty → one empty line)
	return lines
}

// textWrapCount is the number of terminal lines `s` occupies when hard-wrapped
// to `width` cells. Cheap (no allocation); used to size the diff layout for
// every row so the scroll window and scrollbar stay accurate.
func textWrapCount(s string, width int) int {
	if width < 1 {
		width = 1
	}
	lines, w := 1, 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > width && w > 0 {
			lines++
			w = 0
		}
		w += rw
	}
	return lines
}

// spansWrapCount is like textWrapCount but iterates styled spans without
// concatenating their text, so it is allocation-free.
func spansWrapCount(spans []span, width int) int {
	if width < 1 {
		width = 1
	}
	lines, w := 1, 0
	for _, sp := range spans {
		for _, r := range sp.text {
			rw := runeWidth(r)
			if w+rw > width && w > 0 {
				lines++
				w = 0
			}
			w += rw
		}
	}
	return lines
}
