package programstatus

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSequence(t *testing.T) {
	for _, state := range []string{"idle", "working", "done", "blocked", "error"} {
		r := Report{State: state, Kind: "permission", Message: "Generate: résumé; now"}
		got := r.Sequence()
		want := "\x1b]7501;state=" + state + ":app=gojo"
		if state == "blocked" {
			want += ":kind=permission"
		}
		want += ":msg=" + base64.StdEncoding.EncodeToString([]byte(r.Message)) + "\x1b\\"
		if got != want {
			t.Fatalf("%s: got %q, want %q", state, got, want)
		}
		if strings.Contains(got, "progress=") {
			t.Fatal("indeterminate work must omit progress")
		}
	}
	if got := (Report{State: "working:app=other"}).Sequence(); got != "" {
		t.Fatalf("invalid state emitted %q", got)
	}
}

func TestMessageLimits(t *testing.T) {
	for _, input := range []string{
		"line\nwith\ttab\x1b]0;title\x07\u0085\x7f\xff",
		strings.Repeat("界", 2000),
	} {
		sequence := (Report{State: "error", Message: input}).Sequence()
		if len(sequence) > 4096 {
			t.Fatal("sequence exceeds OSC 7501 limit")
		}
		encoded := strings.TrimSuffix(strings.SplitN(sequence, ":msg=", 2)[1], "\x1b\\")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(encoded) > 2732 || len(decoded) > 2048 || !utf8.Valid(decoded) {
			t.Fatalf("invalid message: %d encoded, %d decoded, %v", len(encoded), len(decoded), err)
		}
		for _, c := range string(decoded) {
			if c < 0x20 || c >= 0x7f && c <= 0x9f {
				t.Fatalf("control character %U in message", c)
			}
		}
	}
}
