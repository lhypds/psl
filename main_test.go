package main

import (
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		want  options
		isErr bool
	}{
		{name: "file only", args: []string{"a.psl"}, want: options{path: "a.psl"}},
		{name: "flag after file", args: []string{"a.psl", "--image", "b64"}, want: options{path: "a.psl", image: "b64"}},
		{name: "flag before file", args: []string{"--image", "b64", "a.psl"}, want: options{path: "a.psl", image: "b64"}},
		{name: "equals form", args: []string{"a.psl", "--image=b64"}, want: options{path: "a.psl", image: "b64"}},
		{name: "short form", args: []string{"-i", "b64", "a.psl"}, want: options{path: "a.psl", image: "b64"}},
		{name: "help", args: []string{"--help"}, want: options{help: true}},
		{name: "help wins over a file", args: []string{"a.psl", "-h"}, want: options{path: "a.psl", help: true}},
		{name: "version", args: []string{"-v"}, want: options{version: true}},
		{name: "no file", args: nil, isErr: true},
		{name: "two files", args: []string{"a.psl", "b.psl"}, isErr: true},
		{name: "unknown flag", args: []string{"a.psl", "--loud"}, isErr: true},
		{name: "image without a value", args: []string{"a.psl", "--image"}, isErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseArgs(tc.args)
			if tc.isErr {
				if err == nil {
					t.Fatalf("parseArgs(%q) = %+v, want an error", tc.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs(%q) error: %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("parseArgs(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	long := "write a function that does something very specific — 変換 — and quite long indeed, well past the limit"
	got := summarize(long)
	if n := len([]rune(got)); n != 73 { // 72 runes plus the ellipsis
		t.Errorf("summarize() = %q (%d runes), want 73", got, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("summarize() = %q, want it to end in an ellipsis", got)
	}
	if got := summarize("do\n  the\tthing"); got != "do the thing" {
		t.Errorf("summarize() = %q, want whitespace collapsed", got)
	}
}
