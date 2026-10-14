// Package programstatus encodes gojo's OSC 7501 program status reports.
package programstatus

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

type Report struct {
	State   string
	Kind    string
	Message string
}

// Sequence keeps free text within the protocol's single-line UTF-8 limits.
func (r Report) Sequence() string {
	switch r.State {
	case "idle", "working", "done", "blocked", "error":
	default:
		return ""
	}
	var out strings.Builder
	out.WriteString("\x1b]7501;state=" + r.State + ":app=gojo")
	if r.State == "blocked" {
		switch r.Kind {
		case "permission", "question", "auth":
			out.WriteString(":kind=" + r.Kind)
		}
	}
	message := strings.ToValidUTF8(r.Message, "�")
	message = strings.Map(func(c rune) rune {
		if c < 0x20 || c >= 0x7f && c <= 0x9f {
			return ' '
		}
		return c
	}, message)
	if len(message) > 2048 {
		message = message[:2048]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	if message != "" {
		out.WriteString(":msg=" + base64.StdEncoding.EncodeToString([]byte(message)))
	}
	out.WriteString("\x1b\\")
	return out.String()
}
