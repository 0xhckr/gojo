package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

// Same two-line marker template gojo feeds to `jj log` (see jj.logTemplate).
const verifyTemplate = `"\x01" ++ change_id.short(8) ++ "|" ++ change_id.shortest() ++ "|" ++ commit_id.short(8) ++ "|" ++ commit_id.shortest() ++ "|" ++ author.email() ++ "|" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++ if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "\n" ++ "\x01" ++ description.first_line() ++ "\n"`

// graphPrefixes returns the ordered graph-column of a raw jj template dump:
// everything left of the \x01 marker on each line (right-trimmed). This is the
// canonical layout jj itself produced.
func graphPrefixes(raw string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if idx := strings.IndexByte(line, '\x01'); idx >= 0 {
			out = append(out, strings.TrimRight(line[:idx], " "))
		} else {
			out = append(out, strings.TrimRight(line, " ")) // graph-only line
		}
	}
	return out
}

// gojoPrefixes reconstructs the graph column gojo will render, in render order:
// for each commit its header prefix, body prefix, then trailing edge lines.
func gojoPrefixes(entries []jj.LogEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimRight(e.HeaderPrefix, " "))
		out = append(out, strings.TrimRight(e.BodyPrefix, " "))
		for _, edge := range e.EdgeLines {
			out = append(out, strings.TrimRight(edge, " "))
		}
	}
	return out
}

func runJJ(t *testing.T, jjPath, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(jjPath, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("jj %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// TestVerifyAgainstJJLog drives gojo's real parse+render path over a repo and
// asserts the graph column it produces is byte-identical to jj's own layout,
// for both the full graph and an elided revset subset.
//
// Set GOJO_TEST_REPO to a jj repo path; GOJO_TEST_REVSET overrides the subset.
func TestVerifyAgainstJJLog(t *testing.T) {
	repo := os.Getenv("GOJO_TEST_REPO")
	if repo == "" {
		t.Skip("set GOJO_TEST_REPO to a jj repo path")
	}
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		t.Fatalf("jj not found: %v", err)
	}
	r := jj.NewRunner(jj.Config{JJPath: jjPath, RepoRoot: repo})

	revset := os.Getenv("GOJO_TEST_REVSET")
	cases := []struct {
		name   string
		revset string
	}{
		{"full", ""},
		{"elided", revset},
	}

	for _, tc := range cases {
		if tc.name == "elided" && tc.revset == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			entries, err := r.LogRevset(tc.revset, 100)
			if err != nil {
				t.Fatalf("LogRevset: %v", err)
			}

			args := []string{"log", "--color", "never", "-T", verifyTemplate}
			if tc.revset != "" {
				args = append(args, "-r", tc.revset)
			} else {
				args = append(args, "-n", "100")
			}
			truth := graphPrefixes(runJJ(t, jjPath, repo, args...))
			got := gojoPrefixes(entries)

			// Visual: gojo's actual rendered graph column (full window).
			lines := renderLog(120, 400, entries, 0, 0, nil, 0, rebaseView{}, squashView{}, bookmarkDragView{}, -1)
			var rendered []string
			for _, l := range lines {
				if s := strings.TrimRight(ansi.Strip(l), " "); s != "" {
					rendered = append(rendered, s)
				}
			}
			t.Logf("\n--- gojo rendered ---\n%s", strings.Join(rendered, "\n"))

			if len(got) != len(truth) {
				t.Fatalf("line count: gojo=%d jj=%d\ngojo: %q\njj:   %q", len(got), len(truth), got, truth)
			}
			for i := range truth {
				if got[i] != truth[i] {
					t.Errorf("line %d graph mismatch:\n  gojo: %q\n  jj:   %q", i, got[i], truth[i])
				}
			}
		})
	}
}
