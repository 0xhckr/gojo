package ui

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"gojo/internal/jj"
)

// Drain finite model commands, including Bubble Tea's private sequence message.
// Raw output is captured instead of being written to the test's terminal.
func drainImageCommands(t *testing.T, m Model, cmd tea.Cmd) (Model, string) {
	t.Helper()
	if cmd == nil {
		return m, ""
	}
	msg := cmd()
	if raw, ok := msg.(tea.RawMsg); ok {
		output := fmt.Sprint(raw.Msg)
		if strings.HasSuffix(output, kittyQuery) {
			// Simulate the terminal's transfer fence after accepting all bytes.
			next, follow := imageStep(t, m, uv.KittyGraphicsEvent{Options: kitty.Options{ID: kittyQueryID}, Payload: []byte("OK")})
			return next, output + follow
		}
		return m, output
	}
	v := reflect.ValueOf(msg)
	if v.IsValid() && v.Kind() == reflect.Slice {
		var out strings.Builder
		for i := 0; i < v.Len(); i++ {
			child, ok := v.Index(i).Interface().(tea.Cmd)
			if !ok {
				t.Fatalf("unexpected command message %T", msg)
			}
			var text string
			m, text = drainImageCommands(t, m, child)
			out.WriteString(text)
		}
		return m, out.String()
	}
	next, follow := m.Update(msg)
	return drainImageCommands(t, next.(Model), follow)
}

func imageStep(t *testing.T, m Model, msg tea.Msg) (Model, string) {
	t.Helper()
	next, cmd := m.Update(msg)
	return drainImageCommands(t, next.(Model), cmd)
}

func pngFixture(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageDiffHistoricalSides(t *testing.T) {
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		t.Skip("jj is not installed")
	}
	repo := t.TempDir()
	t.Setenv("JJ_CONFIG", t.TempDir())
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(jjPath, args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	red := pngFixture(t, color.NRGBA{R: 255, A: 128})
	blue := pngFixture(t, color.NRGBA{B: 255, A: 255})
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, path), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "--no-colocate")
	for _, path := range []string{"changed image.png", "removed.png", "old.png"} {
		write(path, red)
	}
	run("new", "-m", "image updates")
	write("changed image.png", blue)
	write("added.png", pngFixture(t, color.NRGBA{G: 255, A: 255}))
	write("broken.png", []byte("not a PNG"))
	if err := os.Remove(filepath.Join(repo, "removed.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repo, "old.png"), filepath.Join(repo, "renamed.png")); err != nil {
		t.Fatal(err)
	}
	commit := run("log", "-r", "@", "--no-graph", "-T", "commit_id")
	run("new", "-m", "unrelated working copy")
	write("changed image.png", red) // must not leak into the historical preview
	m := NewModel()
	m.ready, m.width, m.height = true, 100, 35
	m.cfg = jj.Config{JJPath: jjPath, RepoRoot: repo}
	m.runner = jj.NewRunner(m.cfg)
	m.entries, err = m.runner.Log(10)
	if err != nil {
		t.Fatal(err)
	}
	next, cmd := m.openRevisionDiff(commit, commit, 8, "image updates")
	m, _ = drainImageCommands(t, next.(Model), cmd)
	pairs := make(map[string]*imagePair)
	for _, row := range m.diffRows {
		if row.kind == rowImage {
			pairs[row.path] = row.image
		}
	}
	for _, path := range []string{"changed image.png", "added.png", "removed.png", "renamed.png", "broken.png"} {
		if pairs[path] == nil {
			t.Fatalf("missing preview for %q: %+v", path, m.diffRows)
		}
	}
	assertPixel := func(side imageSide, want color.NRGBA) {
		t.Helper()
		img, err := png.Decode(bytes.NewReader(side.png))
		if err != nil {
			t.Fatalf("preview decode: %v (side error: %s)", err, side.err)
		}
		got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA)
		if got != want {
			t.Fatalf("preview pixel %v, want %v", got, want)
		}
	}
	assertPixel(pairs["changed image.png"].before, color.NRGBA{R: 255, A: 128})
	assertPixel(pairs["changed image.png"].after, color.NRGBA{B: 255, A: 255})
	assertPixel(pairs["renamed.png"].before, color.NRGBA{R: 255, A: 128})
	if pairs["added.png"].before.label != "not present" || pairs["removed.png"].after.label != "not present" || pairs["broken.png"].after.err == "" {
		t.Fatalf("missing added/deleted/invalid image state: added=%q removed=%q broken=%q", pairs["added.png"].before.label, pairs["removed.png"].after.label, pairs["broken.png"].after.err)
	}
	old := pairs["changed image.png"]
	m, _ = imageStep(t, m, imagesLoadedMsg{rev: "stale", raw: m.diffSrcRaw, images: map[string]*imagePair{"changed image.png": {}}})
	for _, row := range m.diffRows {
		if row.path == "changed image.png" && row.kind == rowImage && row.image != old {
			t.Fatal("stale image load replaced current preview")
		}
	}
	refresh := m.openDiffCmd(commit, commit)().(diffLoadedMsg)
	if !refresh.unchanged {
		t.Fatal("unchanged refresh would reload image data")
	}
}

func TestImageGraphicsLifecycle(t *testing.T) {
	m := NewModel()
	m.ready, m.width, m.height = true, 100, 35
	m.entries = []jj.LogEntry{{ChangeID: "test"}}
	m.diffOpen, m.diffIsRevision, m.diffRev = true, true, "test"
	m.diffImageCommit = "test"
	pair := &imagePair{before: decodeImage(pngFixture(t, color.NRGBA{R: 255, A: 128})), after: decodeImage(pngFixture(t, color.NRGBA{B: 255, A: 255}))}
	m.diffRows = []diffRow{{kind: rowFileHeader, path: "image.png"}, {kind: rowImage, path: "image.png", image: pair}}
	m.computeDiffLayout()
	if view := m.View().Content; strings.Contains(view, "\U0010eeee") || !strings.Contains(ansi.Strip(view), "Kitty graphics required") {
		t.Fatal("unsupported terminal must have a readable fallback")
	}
	next, cmd := m.Update(uv.KittyGraphicsEvent{Options: kitty.Options{ID: kittyQueryID}, Payload: []byte("OK")})
	m = next.(Model)
	if !strings.Contains(m.View().Content, "\U0010eeee") {
		t.Fatal("placeholders must share the upload frame before its terminal acknowledgement")
	}
	m, output := drainImageCommands(t, m, cmd)
	if strings.Count(output, "a=t") != 2 || !strings.Contains(m.View().Content, "\U0010eeee") {
		t.Fatal("successful query did not upload and display both sides")
	}
	if strings.Count(output, "a=p") != 4 {
		t.Fatal("terminal acknowledgement must refresh both placements after the placeholder frame")
	}
	for _, width := range []int{35, 80, 140} {
		m, output = imageStep(t, m, tea.WindowSizeMsg{Width: width, Height: 35})
		if strings.Contains(output, "a=t") || !strings.Contains(output, "a=p") {
			t.Fatal("resize must reuse image data and resize placements")
		}
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) != width {
				t.Fatalf("width %d: line has width %d", width, ansi.StringWidth(line))
			}
		}
	}
	m, output = imageStep(t, m, tea.ResumeMsg{})
	if strings.Count(output, "a=t") != 2 {
		t.Fatal("resume must restore images lost with the alternate screen")
	}
	m, output = imageStep(t, m, spinnerTickMsg{})
	if output != "" {
		t.Fatal("ordinary redraw must not retransmit images")
	}
	// A sticky header must replace image cells, not overlap a graphics overlay.
	m.diffScrollY = m.diffHeadLen() + 4
	view := strings.Split(m.View().Content, "\n")
	if strings.Contains(view[3], "\U0010eeee") || !strings.Contains(ansi.Strip(view[3]), "image.png") {
		t.Fatal("sticky file header is covered by image pixels")
	}
	m, output = imageStep(t, m, keyPress("?"))
	if strings.Contains(m.View().Content, "\U0010eeee") || strings.Count(output, "a=d,d=I") != 2 {
		t.Fatal("help view did not remove image cells and free image data")
	}
}

func TestImageDecodeLimits(t *testing.T) {
	data := pngFixture(t, color.NRGBA{A: 255})
	// A valid PNG header advertising huge dimensions must fail before decode.
	binary.BigEndian.PutUint32(data[16:20], 100_000)
	binary.BigEndian.PutUint32(data[20:24], 100_000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if side := decodeImage(data); side.err != "image exceeds 24 megapixels" || side.id != 0 {
		t.Fatal("oversized image was not rejected before decoding")
	}
	if side := decodeImage([]byte("not an image")); side.err == "" || side.id != 0 {
		t.Fatal("invalid image was accepted")
	}
}

func TestKittyChunkedPNG(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 2, 255}, 4000)
	stream := kittyTransmit(imageSide{id: 42, png: data}, 30)
	var encoded strings.Builder
	chunks := strings.Split(strings.TrimSuffix(stream, "\x1b\\"), "\x1b\\")
	chunks = chunks[:len(chunks)-1] // final command creates the virtual placement
	for i, chunk := range chunks {
		header, payload, ok := strings.Cut(chunk, ";")
		if !ok || len(payload) > 4096 || len(payload)%4 != 0 {
			t.Fatal("invalid Kitty chunk")
		}
		want := "m=1"
		if i == len(chunks)-1 {
			want = "m=0"
		}
		if !strings.HasSuffix(header, want) {
			t.Fatalf("chunk header %q must end in %s", header, want)
		}
		encoded.WriteString(payload)
	}
	got, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("chunking corrupted binary image data")
	}
}
