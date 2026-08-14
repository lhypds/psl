package compiler

import (
	"strings"
	"testing"

	"psl/internal/lang/golang"
	"psl/internal/lang/python"
	"psl/internal/slot"
)

func TestBuildSystem(t *testing.T) {
	s := slot.Slot{Instruction: "write a fibonacci function"}

	got := buildSystem("fib.go.psl", golang.Language, s, "", false)
	for _, want := range []string{"fib.go.psl", "write a fibonacci function", slot.Marker} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "image is attached") {
		t.Error("system prompt mentions an image that was not attached")
	}
	if strings.Contains(got, "Guidance from the author") {
		t.Error("system prompt announces guidance that was not given")
	}

	if !strings.Contains(buildSystem("fib.go.psl", golang.Language, s, "", true), "image is attached") {
		t.Error("system prompt should say when an image is attached")
	}
}

func TestBuildSystemGuidance(t *testing.T) {
	s := slot.Slot{Instruction: "move to the button"}
	const guidance = "move() takes absolute screen coordinates in pixels"

	got := buildSystem("bot.py.psl", python.Language, s, guidance, false)
	if !strings.Contains(got, guidance) {
		t.Errorf("system prompt is missing the guidance:\n%s", got)
	}
	if !strings.Contains(got, "Guidance from the author") {
		t.Errorf("guidance should be labelled as the author's, not left as loose text:\n%s", got)
	}
	// The guidance is context for the file; the instruction is still what says
	// what to write, so it comes last.
	if strings.Index(got, guidance) > strings.Index(got, s.Instruction) {
		t.Errorf("guidance should precede the instruction:\n%s", got)
	}
	// Whitespace-only guidance is the same as none: --prompt "" or a file of
	// blank lines must not announce a briefing that says nothing.
	if strings.Contains(buildSystem("bot.py.psl", python.Language, s, "  \n\t\n", false), "Guidance from the author") {
		t.Error("blank guidance should be treated as no guidance")
	}
}

func TestClean(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{"plain text", "  42  ", "42"},
		{"fenced with language", "```go\nfunc f() {}\n```", "func f() {}"},
		{"fenced without language", "```\nhello\n```", "hello"},
		{"tilde fence", "~~~\nhello\n~~~", "hello"},
		{"fence with trailing blank line", "```go\nx := 1\n```\n\n", "x := 1"},
		{"a fence that closes early is left alone", "```md\ntext\n```go\ncode\n```\n```", "```md\ntext\n```go\ncode\n```\n```"},
		{"markdown prose with a code block is left alone", "Here you go:\n\n```go\nx := 1\n```", "Here you go:\n\n```go\nx := 1\n```"},
		{"backticks inline are kept", "x := `raw`", "x := `raw`"},
		{"internal blank lines survive", "```go\na\n\nb\n```", "a\n\nb"},
		{"leading indentation of the body is preserved", "```go\n\tif x {\n\t\ty()\n\t}\n```", "\tif x {\n\t\ty()\n\t}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clean(tc.out); got != tc.want {
				t.Errorf("clean() = %q, want %q", got, tc.want)
			}
		})
	}
}
