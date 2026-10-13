package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"gojo/internal/jj"
)

const imageHeight = 12
const kittyQueryID = 4294967294
const kittyQuery = "\x1b_Ga=q,i=4294967294,s=1,v=1,t=d,f=24;AAAA\x1b\\"

// Unicode placements follow the same clipping and scrolling as text. Randomize
// the process's ID range to avoid collisions with other terminal applications.
var nextImageID atomic.Uint32
var imageIDSeed = rand.Uint32() & 0x3fffffff

type imageSide struct {
	id    uint32
	png   []byte
	label string
	err   string
}

type imagePair struct {
	before imageSide
	after  imageSide
}

type imagesLoadedMsg struct {
	rev, raw, commit string
	images           map[string]*imagePair
}

type imagePlacement struct {
	id   uint32
	cols int
}

type imageTerminal struct {
	supported bool
	busy      bool
	refresh   bool
	placed    []imagePlacement
	pending   []imagePlacement
}

func imagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".avif", ".ico":
		return true
	}
	return false
}

func decodeImage(data []byte) imageSide {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imageSide{err: "unsupported or invalid image"}
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 24_000_000 {
		return imageSide{err: "image exceeds 24 megapixels"}
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return imageSide{err: "cannot decode image"}
	}
	// Bound terminal transfers independently of the source dimensions.
	w, h := cfg.Width, cfg.Height
	if max(w, h) > 1200 {
		scale := 1200.0 / float64(max(w, h))
		w, h = max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
		src = dst
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		return imageSide{err: "cannot encode preview"}
	}
	serial := nextImageID.Add(1)
	if serial > 0x3fffffff {
		return imageSide{err: "image ID limit reached; restart gojo"}
	}
	id := imageIDSeed + serial
	label := fmt.Sprintf("%s · %d×%d", strings.ToUpper(format), cfg.Width, cfg.Height)
	if format == "gif" {
		label += " · first frame"
	}
	return imageSide{id: id, png: buf.Bytes(), label: label}
}

func loadImageSide(r *jj.Runner, rev, path string) imageSide {
	data, err := r.ImageFile(rev, path)
	if err != nil {
		return imageSide{err: "cannot read image: " + err.Error()}
	}
	return decodeImage(data)
}

func (m Model) loadImagesCmd() tea.Cmd {
	r, rev, raw, commit := m.runner, m.diffRev, m.diffSrcRaw, m.diffImageCommit
	rows := m.diffRows
	return func() tea.Msg {
		images := make(map[string]*imagePair)
		parent, parentErr := r.ImageParent(commit)
		for _, row := range rows {
			if row.kind != rowImage {
				continue
			}
			pair := &imagePair{}
			if row.changeType == "added" {
				pair.before.label = "not present"
			} else if parentErr != nil {
				pair.before.err = parentErr.Error()
			} else {
				path := row.path
				if row.prevPath != "" {
					path = row.prevPath
				}
				pair.before = loadImageSide(r, parent, path)
			}
			if row.changeType == "deleted" {
				pair.after.label = "not present"
			} else {
				pair.after = loadImageSide(r, commit, row.path)
			}
			images[row.path] = pair
		}
		return imagesLoadedMsg{rev: rev, raw: raw, commit: commit, images: images}
	}
}

func hasImageRows(rows []diffRow) bool {
	for _, row := range rows {
		if row.kind == rowImage {
			return true
		}
	}
	return false
}

func preserveImages(rows, old []diffRow) {
	for i := range rows {
		if rows[i].kind == rowImage && i < len(old) && old[i].path == rows[i].path {
			rows[i].image = old[i].image
			rows[i].imageCols = old[i].imageCols
		}
	}
}

func imageColumns(width int) int {
	return max(1, min(len(imageDiacritics), (width-4)/2))
}

func kittyTransmit(side imageSide, cols int) string {
	encoded := base64.StdEncoding.EncodeToString(side.png)
	var out strings.Builder
	first := true
	for len(encoded) > 0 {
		n := min(len(encoded), 4096)
		more := 0
		if n < len(encoded) {
			more = 1
		}
		if first {
			fmt.Fprintf(&out, "\x1b_Ga=t,t=d,f=100,q=2,i=%d,m=%d;", side.id, more)
			first = false
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d;", more)
		}
		out.WriteString(encoded[:n])
		out.WriteString("\x1b\\")
		encoded = encoded[n:]
	}
	out.WriteString(kittyPlace(side.id, cols))
	return out.String()
}

func kittyPlace(id uint32, cols int) string {
	return fmt.Sprintf("\x1b_Ga=p,U=1,p=1,z=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, imageHeight)
}

func kittyDelete(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

func (m *Model) applyImageMessage(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.ResumeMsg, describeFinishedMsg, squashFinishedMsg:
		// Leaving the alternate screen for an editor clears Kitty's images.
		m.images.placed = nil
		m.images.pending = nil
	case uv.KittyGraphicsEvent:
		if msg.Options.ID == kittyQueryID && string(msg.Payload) == "OK" {
			m.images.supported = true
			if m.images.busy {
				m.images.busy = false
				m.images.placed = m.images.pending
				m.images.pending = nil
				m.images.refresh = true
			}
		}
	case imagesLoadedMsg:
		if m.diffOpen && msg.rev == m.diffRev && msg.raw == m.diffSrcRaw && msg.commit == m.diffImageCommit {
			rows := append([]diffRow(nil), m.diffRows...)
			for i := range rows {
				if rows[i].kind == rowImage {
					rows[i].image = msg.images[rows[i].path]
				}
			}
			m.diffRows = rows
		}
	}
}

// Only one transfer is in flight, so a late resize cannot resurrect an old
// placement. Visible images are retained; offscreen image data is released.
func (m *Model) syncImages() tea.Cmd {
	if !m.images.supported {
		return nil
	}
	cols := imageColumns(m.diffLayout.scrollW)
	if m.images.busy {
		m.setImageColumns(m.images.pending, cols)
		return nil
	}
	visible := m.diffOpen && m.diffIsRevision && !m.workspaceOpen && !m.themeOpen && !m.conflictOpen && m.view != viewHelp
	desired := make(map[uint32]imageSide)
	if visible {
		for i, row := range m.diffRows {
			if row.kind == rowImage && row.image != nil && m.imageRowVisible(i) {
				for _, side := range []imageSide{row.image.before, row.image.after} {
					if side.id != 0 {
						desired[side.id] = side
					}
				}
			}
		}
	}
	placed := make(map[uint32]int)
	for _, p := range m.images.placed {
		placed[p.id] = p.cols
	}
	var out strings.Builder
	var next []imagePlacement
	changed := false
	refresh := m.images.refresh
	m.images.refresh = false
	for id := range placed {
		if _, ok := desired[id]; !ok {
			out.WriteString(kittyDelete(id))
			changed = true
		}
	}
	for id, side := range desired {
		if placed[id] == 0 {
			out.WriteString(kittyTransmit(side, cols))
			changed = true
		} else if placed[id] != cols || refresh {
			out.WriteString(kittyPlace(id, cols))
			changed = changed || placed[id] != cols
		}
		next = append(next, imagePlacement{id: id, cols: cols})
	}
	// Pending placements and their placeholders share the same output frame.
	m.setImageColumns(next, cols)
	if out.Len() == 0 {
		return nil
	}
	if !changed {
		// Kitty can select text-only rendering before scanning new placeholder
		// cells. Refresh placements in the following frame to enable image layers.
		return tea.Raw(out.String())
	}
	m.images.busy = true
	m.images.pending = next
	// Fence the transfer with a terminal response, not a local command result:
	// Raw output is buffered and may not have reached the terminal yet.
	return tea.Raw(out.String() + kittyQuery)
}

func (m *Model) setImageColumns(placements []imagePlacement, cols int) {
	placed := make(map[uint32]int, len(placements))
	for _, p := range placements {
		placed[p.id] = p.cols
	}
	rows := m.diffRows
	copied := false
	for i, row := range rows {
		if row.kind != rowImage || row.image == nil {
			continue
		}
		want := cols
		for _, side := range []imageSide{row.image.before, row.image.after} {
			if side.id != 0 && placed[side.id] != cols {
				want = -1
			}
		}
		if row.imageCols != want {
			if !copied {
				rows = append([]diffRow(nil), rows...)
				copied = true
			}
			rows[i].imageCols = want
		}
	}
	m.diffRows = rows
}

func (m Model) imageRowVisible(i int) bool {
	if m.diffCollapsed[m.diffRows[i].path] || i >= len(m.diffLayout.starts) {
		return false
	}
	start := m.diffHeadLen() + m.diffLayout.starts[i]
	end := start + m.diffLayout.counts[i]
	return end > m.diffScrollY && start < m.diffScrollY+max(0, m.contentHeight()-1)
}

// First 128 entries of Kitty's gen/rowcolumn-diacritics.txt. Every cell carries
// both coordinates so partial repaints and menus cannot change their meaning.
var imageDiacritics = []rune{
	0x0305, 0x030d, 0x030e, 0x0310, 0x0312, 0x033d, 0x033e, 0x033f,
	0x0346, 0x034a, 0x034b, 0x034c, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035b, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036a, 0x036b, 0x036c, 0x036d, 0x036e, 0x036f, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059c, 0x059d, 0x059e, 0x059f, 0x05a0, 0x05a1,
	0x05a8, 0x05a9, 0x05ab, 0x05ac, 0x05af, 0x05c4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065a, 0x065b, 0x065d, 0x065e, 0x06d6, 0x06d7, 0x06d8,
	0x06d9, 0x06da, 0x06db, 0x06dc, 0x06df, 0x06e0, 0x06e1, 0x06e2,
	0x06e4, 0x06e7, 0x06e8, 0x06eb, 0x06ec, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073a, 0x073d, 0x073f, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074a, 0x07eb, 0x07ec, 0x07ed, 0x07ee,
	0x07ef, 0x07f0, 0x07f1, 0x07f3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081b, 0x081c, 0x081d, 0x081e, 0x081f, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082a, 0x082b, 0x082c,
}

func imagePlaceholder(id uint32, row, cols int) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm\x1b[58;2;0;0;1m", id>>16&255, id>>8&255, id&255)
	for col := range cols {
		out.WriteRune('\U0010eeee')
		out.WriteRune(imageDiacritics[row])
		out.WriteRune(imageDiacritics[col])
		out.WriteRune(imageDiacritics[id>>24])
	}
	out.WriteString("\x1b[39;59m")
	return out.String()
}

func renderImageRow(width int, row diffRow, sub int) string {
	cols := imageColumns(width)
	var before, after imageSide
	if row.image != nil {
		before, after = row.image.before, row.image.after
	}
	renderSide := func(title string, side imageSide) string {
		text := ""
		switch {
		case sub == 0:
			text = title
			if side.label != "" {
				text += " · " + side.label
			}
		case sub == 1 && side.err != "":
			text = side.err
		case sub == 1 && row.image == nil:
			text = "loading image…"
		case sub >= 1 && sub <= imageHeight && side.id != 0 && row.imageCols == cols:
			return imagePlaceholder(side.id, sub-1, cols)
		case sub == 1 && side.id != 0:
			text = "Kitty graphics required"
			if row.imageCols < 0 {
				text = "loading preview…"
			}
		}
		return bgRow(cols, colPanel, seg{text: expandTabs(text), fg: colTextMuted, bg: colPanel})
	}
	return bgRow(width, colPanel, seg{text: " " + renderSide("before", before) + " │ " + renderSide("after", after), bg: colPanel})
}
