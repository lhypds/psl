package compiler

import "testing"

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
