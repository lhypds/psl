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

	got := buildSystem("fib.go.psl", golang.Language, s, "", false, false)
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

	if !strings.Contains(buildSystem("fib.go.psl", golang.Language, s, "", true, false), "image is attached") {
		t.Error("system prompt should say when an image is attached")
	}
}

// Where the marker stands is what decides between the two rules that would
// otherwise compete: a question inside a statement is answered as a literal,
// and the same question standing where a statement goes is the work to write.
// Without the position said outright, ':: calculate 360 x 360 ::' alone on its
// line resolves to '129600' — an answer where the file needed statements.
func TestBuildSystemSaysWhatThePositionOfTheMarkerMeans(t *testing.T) {
	got := buildSystem("main.macro.psl", nil, slot.Slot{Instruction: "calculate 360 x 360"}, "", false, false)
	for _, want := range []string{
		"Where the marker stands is how much of the program it is",
		"Alone on its line, with no expression around it to be part of, it stands for whole statements",
		"the wording of the instruction never overrides it",
		"where the marker stands for a value",
		"So is every instruction at a marker that stands for statements",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt is missing %q:\n%s", want, got)
		}
	}
}

func TestBuildSystemGuidance(t *testing.T) {
	s := slot.Slot{Instruction: "move to the button"}
	const guidance = "move() takes absolute screen coordinates in pixels"

	got := buildSystem("bot.py.psl", python.Language, s, guidance, false, false)
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
	if strings.Contains(buildSystem("bot.py.psl", python.Language, s, "  \n\t\n", false, false), "Guidance from the author") {
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
