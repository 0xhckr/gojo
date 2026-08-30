package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestVersionFlags(t *testing.T) {
	oldVersion := version
	version = "1.2.3"
	t.Cleanup(func() { version = oldVersion })

	for _, arg := range []string{"--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var stdout bytes.Buffer
			handled, err := handleFlags([]string{arg}, &stdout, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if !handled {
				t.Fatal("version flag was not handled")
			}
			if got, want := stdout.String(), "gojo 1.2.3\n"; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestChangelogFlag(t *testing.T) {
	var stdout bytes.Buffer
	handled, err := handleFlags([]string{"--changelog"}, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("changelog flag was not handled")
	}
	if got := stdout.String(); got != changelog {
		t.Fatal("changelog output does not match embedded CHANGELOG.md")
	}
	if !strings.HasPrefix(stdout.String(), "# Changelog\n") {
		t.Fatal("changelog output is missing its heading")
	}
}

func TestNoOutputFlag(t *testing.T) {
	var stdout bytes.Buffer
	handled, err := handleFlags(nil, &stdout, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("empty arguments should launch the TUI")
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected output %q", stdout.String())
	}
}
