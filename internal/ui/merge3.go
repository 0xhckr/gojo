package ui

// merge3.go — three-way merge on line slices for the conflict resolution view.
//
// The inputs are the three full file versions jj hands a merge tool (left =
// side #1, base = common ancestor, right = side #2), each split into lines
// with trailing newlines preserved so the resolved file can be recomposed
// byte-exactly.
//
// merge3 walks a line-level diff on both sides and groups overlapping changed
// regions into blocks:
//
//   - mergeContext: regions neither side touched (left == base == right).
//   - mergeAuto: regions only one side changed (or both changed identically);
//     auto-merged like a real merge — auto holds the winning lines.
//   - mergeConflict: regions both sides changed differently; the user picks
//     left, right, or both per block and composeResolved emits the result.
//
// Grouping rules follow diff3: hunks merge when their base ranges overlap
// strictly, plus the one boundary case that matters — insertions by both
// sides at exactly the same base position merge into one group (so add/add at
// the same line conflicts instead of silently duplicating). Hunks that merely
// touch (one ends where another starts) stay separate and auto-merge cleanly.

import (
	"errors"
	"strings"
)

type mergeBlockKind int

const (
	mergeContext mergeBlockKind = iota
	mergeAuto
	mergeConflict
)

// resolution is the per-conflict-block user choice.
type resolution int

const (
	resolutionUnset resolution = iota
	resolutionLeft             // take the left side's lines
	resolutionRight            // take the right side's lines
	resolutionBoth             // left lines, then right lines
)

// mergeBlock is one region of a three-way merge. The slices are views into
// the caller's side contents. auto holds the auto-merged lines for mergeAuto
// blocks; choice is only meaningful for mergeConflict blocks.
type mergeBlock struct {
	kind     mergeBlockKind
	left     []string
	base     []string
	right    []string
	auto     []string
	autoLeft bool // mergeAuto: which side's lines auto holds (display tint)
	choice   resolution
}

// conflictCount returns the number of conflict blocks in the slice.
func conflictCount(blocks []mergeBlock) int {
	n := 0
	for _, b := range blocks {
		if b.kind == mergeConflict {
			n++
		}
	}
	return n
}

// unresolvedCount returns the number of conflict blocks still lacking a
// resolution choice.
func unresolvedCount(blocks []mergeBlock) int {
	n := 0
	for _, b := range blocks {
		if b.kind == mergeConflict && b.choice == resolutionUnset {
			n++
		}
	}
	return n
}

var errUnresolved = errors.New("unresolved conflict blocks remain")

// composeResolved renders the resolved file content from blocks. Every
// conflict block must carry a choice; otherwise it returns errUnresolved.
func composeResolved(blocks []mergeBlock) (string, error) {
	var sb strings.Builder
	for _, b := range blocks {
		switch b.kind {
		case mergeContext:
			for _, l := range b.base {
				sb.WriteString(l)
			}
		case mergeAuto:
			for _, l := range b.auto {
				sb.WriteString(l)
			}
		case mergeConflict:
			switch b.choice {
			case resolutionLeft:
				for _, l := range b.left {
					sb.WriteString(l)
				}
			case resolutionRight:
				for _, l := range b.right {
					sb.WriteString(l)
				}
			case resolutionBoth:
				for _, l := range b.left {
					sb.WriteString(l)
				}
				for _, l := range b.right {
					sb.WriteString(l)
				}
			default:
				return "", errUnresolved
			}
		}
	}
	return sb.String(), nil
}

// maxLineDiffCells caps the LCS matrix for line-level diffing (budget int32
// cells ≈ 16 MiB). Sides whose trimmed middle exceeds this degrade to a
// single whole-file change region rather than risking a huge allocation.
const maxLineDiffCells = 1 << 22

// hunkOp is one changed region of a base→side line diff. A zero-width base
// range is a pure insertion; a zero-width side range a pure deletion.
type hunkOp struct{ bLo, bHi, sLo, sHi int }

// diffLines computes the changed regions between base and side using LCS on
// lines (exact string equality, trailing newlines included). The common
// prefix and suffix are trimmed before the matrix runs.
func diffLines(base, side []string) []hunkOp {
	lo := 0
	for lo < len(base) && lo < len(side) && base[lo] == side[lo] {
		lo++
	}
	hiB, hiS := len(base), len(side)
	for hiB > lo && hiS > lo && base[hiB-1] == side[hiS-1] {
		hiB--
		hiS--
	}
	mm, nn := hiB-lo, hiS-lo
	switch {
	case mm == 0 && nn == 0:
		return nil
	case mm == 0:
		return []hunkOp{{bLo: lo, bHi: lo, sLo: lo, sHi: hiS}}
	case nn == 0:
		return []hunkOp{{bLo: lo, bHi: hiB, sLo: lo, sHi: lo}}
	}
	if mm*nn > maxLineDiffCells {
		return []hunkOp{{bLo: lo, bHi: hiB, sLo: lo, sHi: hiS}}
	}

	// Flat LCS matrix over the trimmed middles ((mm+1) x (nn+1)).
	H := nn + 1
	dp := make([]int32, (mm+1)*H)
	for i := 1; i <= mm; i++ {
		row := i * H
		for j := 1; j <= nn; j++ {
			if base[lo+i-1] == side[lo+j-1] {
				dp[row+j] = dp[row-H+j-1] + 1
			} else if dp[row-H+j] >= dp[row+j-1] {
				dp[row+j] = dp[row-H+j]
			} else {
				dp[row+j] = dp[row+j-1]
			}
		}
	}

	// Backtrack to flag matched (common) lines in each middle slice.
	commonB := make([]bool, mm)
	commonS := make([]bool, nn)
	i, j := mm, nn
	for i > 0 && j > 0 {
		if base[lo+i-1] == side[lo+j-1] {
			commonB[i-1] = true
			commonS[j-1] = true
			i--
			j--
		} else if dp[i*H+j-H] >= dp[i*H+j-1] {
			i--
		} else {
			j--
		}
	}

	// Emit change hunks from the middle classifications. Between matched
	// pairs, runs of unmatched base lines and unmatched side lines coalesce
	// into one hunk.
	var hunks []hunkOp
	p, q := 0, 0
	open := false
	var h hunkOp
	for p < mm || q < nn {
		switch {
		case p < mm && q < nn && commonB[p] && commonS[q]:
			if open {
				hunks = append(hunks, h)
				open = false
			}
			p++
			q++
		case p < mm && !commonB[p]:
			if !open {
				open = true
				h = hunkOp{bLo: lo + p, bHi: lo + p, sLo: lo + q, sHi: lo + q}
			}
			h.bHi = lo + p + 1
			p++
		default: // q < nn && !commonS[q]
			if !open {
				open = true
				h = hunkOp{bLo: lo + p, bHi: lo + p, sLo: lo + q, sHi: lo + q}
			}
			h.sHi = lo + q + 1
			q++
		}
	}
	if open {
		hunks = append(hunks, h)
	}
	return hunks
}

// sideDiff is a per-base-line index of one side's diff against base.
type sideDiff struct {
	chg    []bool     // len(base): line belongs to a change run
	rep    [][]string // replacement text at run starts (where skip > 0)
	skip   []int      // run length starting at this base line (0 elsewhere)
	ins    [][]string // len(base)+1: lines inserted before base line i
	mapIdx []int      // len(base): side index for unchanged base lines
}

func buildSideDiff(base, side []string, hunks []hunkOp) sideDiff {
	n := len(base)
	d := sideDiff{
		chg:    make([]bool, n),
		rep:    make([][]string, n),
		skip:   make([]int, n),
		ins:    make([][]string, n+1),
		mapIdx: make([]int, n),
	}
	// Fill mapIdx from the keep gaps between hunks.
	prevB, prevS := 0, 0
	for _, h := range hunks {
		for i := prevB; i < h.bLo; i++ {
			d.mapIdx[i] = prevS + (i - prevB)
		}
		if h.bHi > h.bLo {
			for i := h.bLo; i < h.bHi; i++ {
				d.chg[i] = true
			}
			d.rep[h.bLo] = side[h.sLo:h.sHi]
			d.skip[h.bLo] = h.bHi - h.bLo
		} else {
			d.ins[h.bLo] = side[h.sLo:h.sHi]
		}
		prevB, prevS = h.bHi, h.sHi
	}
	for i := prevB; i < n; i++ {
		d.mapIdx[i] = prevS + (i - prevB)
	}
	return d
}

// region extracts this side's lines covering the base range [lo, hi), with
// every insertion strictly inside the range included. endIns controls whether
// an insertion landing exactly at hi is included (set when the merge group
// absorbed a trailing insertion at that position).
func (d sideDiff) region(side []string, lo, hi int, endIns bool) []string {
	var out []string
	for i := lo; i < hi; i++ {
		out = append(out, d.ins[i]...)
		if d.chg[i] {
			if d.skip[i] > 0 {
				out = append(out, d.rep[i]...)
				i += d.skip[i] - 1 // the loop increment adds one
			}
			continue
		}
		out = append(out, side[d.mapIdx[i]])
	}
	if endIns {
		out = append(out, d.ins[hi]...)
	}
	return out
}

// linesEqual compares two line slices.
func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// splitLinesKeepEnds splits s into lines, each retaining its trailing "\n"
// (except a final line when s does not end with one). Empty s yields nil.
func splitLinesKeepEnds(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// mergeGroup is one merged change region: the union of overlapping side hunks
// in base coordinates, recording which sides contributed and whether a
// side's insertion landed exactly at the group's right edge.
type mergeGroup struct {
	lo, hi  int
	leftIn  bool
	rightIn bool
	endIns  int // bitmask: 1 = left insertion absorbed at hi, 2 = right
}

// merge3 computes the three-way merge of base, left, and right, returning the
// ordered context/auto/conflict blocks spanning the whole file.
func merge3(base, left, right []string) []mergeBlock {
	lh := diffLines(base, left)
	rh := diffLines(base, right)
	ld := buildSideDiff(base, left, lh)
	rd := buildSideDiff(base, right, rh)

	var groups []mergeGroup
	add := func(isLeft bool, h hunkOp) {
		bit := 1
		if !isLeft {
			bit = 2
		}
		if len(groups) > 0 {
			g := &groups[len(groups)-1]
			switch {
			case h.bLo < g.hi:
				// Strict overlap: merge and extend.
			case h.bLo == h.bHi && h.bLo == g.hi && g.endIns != 0:
				// Insertion at the same base position where the open group
				// already absorbed one (both sides inserted at the same
				// point).
			default:
				g = nil
			}
			if g != nil {
				if isLeft {
					g.leftIn = true
				} else {
					g.rightIn = true
				}
				if h.bHi > g.hi {
					g.hi = h.bHi
				}
				if h.bLo == h.bHi && h.bLo == g.hi {
					g.endIns |= bit
				}
				return
			}
		}
		g := mergeGroup{lo: h.bLo, hi: h.bHi}
		if isLeft {
			g.leftIn = true
		} else {
			g.rightIn = true
		}
		if h.bLo == h.bHi {
			// A pure insertion owns its own right edge.
			g.endIns = bit
		}
		groups = append(groups, g)
	}

	i, j := 0, 0
	for i < len(lh) || j < len(rh) {
		// Pick the hunk with the smaller baseLo (tie: left first; the order
		// of sides within a group is immaterial).
		if j >= len(rh) || (i < len(lh) && lh[i].bLo <= rh[j].bLo) {
			add(true, lh[i])
			i++
		} else {
			add(false, rh[j])
			j++
		}
	}

	var blocks []mergeBlock
	p := 0
	for _, g := range groups {
		if g.lo > p {
			c := base[p:g.lo]
			blocks = append(blocks, mergeBlock{kind: mergeContext, left: c, base: c, right: c})
		}
		baseSlice := base[g.lo:g.hi]
		var lSlice, rSlice []string
		if g.leftIn {
			lSlice = ld.region(left, g.lo, g.hi, g.endIns&1 != 0 && g.lo == g.hi)
		} else {
			lSlice = baseSlice
		}
		if g.rightIn {
			rSlice = rd.region(right, g.lo, g.hi, g.endIns&2 != 0 && g.lo == g.hi)
		} else {
			rSlice = baseSlice
		}

		lUnchanged := !g.leftIn || linesEqual(lSlice, baseSlice)
		rUnchanged := !g.rightIn || linesEqual(rSlice, baseSlice)
		switch {
		case lUnchanged && rUnchanged:
			// Defensive: a group must contain a real change, but a side that
			// rewrote identical-to-base content folds to context.
			blocks = append(blocks, mergeBlock{kind: mergeContext, left: baseSlice, base: baseSlice, right: baseSlice})
		case lUnchanged:
			blocks = append(blocks, mergeBlock{kind: mergeAuto, base: baseSlice, auto: rSlice, autoLeft: false})
		case rUnchanged:
			blocks = append(blocks, mergeBlock{kind: mergeAuto, base: baseSlice, auto: lSlice, autoLeft: true})
		case linesEqual(lSlice, rSlice):
			blocks = append(blocks, mergeBlock{kind: mergeAuto, base: baseSlice, auto: lSlice, autoLeft: true})
		default:
			blocks = append(blocks, mergeBlock{kind: mergeConflict, base: baseSlice, left: lSlice, right: rSlice})
		}
		p = g.hi
	}
	if p < len(base) {
		c := base[p:]
		blocks = append(blocks, mergeBlock{kind: mergeContext, left: c, base: c, right: c})
	}
	return blocks
}

// merge3Strings is a convenience wrapper for raw contents.
func merge3Strings(base, left, right string) []mergeBlock {
	return merge3(splitLinesKeepEnds(base), splitLinesKeepEnds(left), splitLinesKeepEnds(right))
}
