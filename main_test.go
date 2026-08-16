package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"psl/internal/psllog"
	"psl/internal/pslrc"
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
		{name: "prompt", args: []string{"a.psl", "--prompt", "move takes pixels"}, want: options{path: "a.psl", prompt: "move takes pixels"}},
		{name: "prompt equals form", args: []string{"--prompt=pixels", "a.psl"}, want: options{path: "a.psl", prompt: "pixels"}},
		{name: "prompt short form", args: []string{"-p", "pixels", "a.psl"}, want: options{path: "a.psl", prompt: "pixels"}},
		{name: "image and prompt together", args: []string{"a.psl", "-i", "b64", "-p", "pixels"}, want: options{path: "a.psl", image: "b64", prompt: "pixels"}},
		{name: "help", args: []string{"--help"}, want: options{help: true}},
		{name: "help wins over a file", args: []string{"a.psl", "-h"}, want: options{path: "a.psl", help: true}},
		{name: "version", args: []string{"-v"}, want: options{version: true}},
		{name: "update command", args: []string{"update"}, want: options{update: true}},
		{name: "config command", args: []string{"config"}, want: options{config: true}},
		{name: "usage command", args: []string{"usage"}, want: options{usage: true}},
		{name: "run command", args: []string{"run", "app.py.psl"}, want: options{run: true, path: "app.py.psl"}},
		{name: "run flags", args: []string{"run", "--prompt", "pixels", "app.py.psl"}, want: options{run: true, path: "app.py.psl", prompt: "pixels"}},
		{name: "run program arguments", args: []string{"run", "app.py.psl", "--", "--name", "Ada"}, want: options{run: true, path: "app.py.psl", programArgs: []string{"--name", "Ada"}}},
		{name: "run without a file", args: []string{"run"}, isErr: true},
		{name: "run separator before file", args: []string{"run", "--", "app.py.psl"}, isErr: true},
		{name: "resolve command", args: []string{"resolve", ":: say hello ::"}, want: options{resolve: true, instruction: ":: say hello ::"}},
		{name: "resolve flags", args: []string{"resolve", ":: say hello ::", "--prompt", "brief"}, want: options{resolve: true, instruction: ":: say hello ::", prompt: "brief"}},
		{name: "resolve runtime context", args: []string{"resolve", ":: greet Ada ::", "--context-file", "/work/app.py.psl", "--context-offset", "24"}, want: options{resolve: true, instruction: ":: greet Ada ::", contextPath: "/work/app.py.psl", contextPathSet: true, contextOffset: 24, contextOffsetSet: true}},
		{name: "resolve without a slot", args: []string{"resolve"}, isErr: true},
		{name: "resolve with two slots", args: []string{"resolve", ":: one ::", ":: two ::"}, isErr: true},
		{name: "resolve context needs offset", args: []string{"resolve", ":: hi ::", "--context-file", "app.py.psl"}, isErr: true},
		{name: "resolve context needs file", args: []string{"resolve", ":: hi ::", "--context-offset", "0"}, isErr: true},
		{name: "resolve context offset is an integer", args: []string{"resolve", ":: hi ::", "--context-file", "app.py.psl", "--context-offset", "line"}, isErr: true},
		{name: "runtime context flags belong to resolve", args: []string{"app.py.psl", "--context-file", "app.py.psl", "--context-offset", "0"}, isErr: true},
		{name: "no file", args: nil, isErr: true},
		{name: "two files", args: []string{"a.psl", "b.psl"}, isErr: true},
		{name: "update takes no arguments", args: []string{"update", "a.psl"}, isErr: true},
		{name: "config takes no arguments", args: []string{"config", "a.psl"}, isErr: true},
		// "update" is a command only in first position, so a file of that name
		// stays compilable as ./update.
		{name: "update after a file is a second file", args: []string{"a.psl", "update"}, isErr: true},
		{name: "path named update", args: []string{"./update"}, want: options{path: "./update"}},
		{name: "config after a file is a second file", args: []string{"a.psl", "config"}, isErr: true},
		{name: "path named config", args: []string{"./config"}, want: options{path: "./config"}},
		{name: "usage takes no arguments", args: []string{"usage", "a.psl"}, isErr: true},
		{name: "usage after a file is a second file", args: []string{"a.psl", "usage"}, isErr: true},
		{name: "path named usage", args: []string{"./usage"}, want: options{path: "./usage"}},
		{name: "unknown flag", args: []string{"a.psl", "--loud"}, isErr: true},
		{name: "image without a value", args: []string{"a.psl", "--image"}, isErr: true},
		{name: "prompt without a value", args: []string{"a.psl", "--prompt"}, isErr: true},
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
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseArgs(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestLoadPrompt(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, "api.md")
	if err := os.WriteFile(brief, []byte("move() takes absolute pixels\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(empty, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("no prompt", func(t *testing.T) {
		if got, err := loadPrompt(""); err != nil || got != "" {
			t.Errorf("loadPrompt(\"\") = %q, %v; want empty and no error", got, err)
		}
	})
	t.Run("text", func(t *testing.T) {
		const text = "move() takes absolute pixels"
		if got, err := loadPrompt(text); err != nil || got != text {
			t.Errorf("loadPrompt(%q) = %q, %v; want the text itself", text, got, err)
		}
	})
	t.Run("file", func(t *testing.T) {
		got, err := loadPrompt(brief)
		if err != nil {
			t.Fatalf("loadPrompt(%q) error: %v", brief, err)
		}
		if got != "move() takes absolute pixels\n" {
			t.Errorf("loadPrompt(%q) = %q, want the file's contents", brief, got)
		}
	})
	t.Run("a directory is not a prompt file", func(t *testing.T) {
		if got, err := loadPrompt(dir); err != nil || got != dir {
			t.Errorf("loadPrompt(%q) = %q, %v; want the argument as text", dir, got, err)
		}
	})
	t.Run("empty file", func(t *testing.T) {
		if _, err := loadPrompt(empty); err == nil {
			t.Errorf("loadPrompt(%q) succeeded, want an error naming the empty file", empty)
		}
	})
}

// The example seeds a new .pslrc when psl config finds none, so a broken one
// would hand the user a file psl refuses to read.
func TestExampleFileParses(t *testing.T) {
	cfg, err := pslrc.Parse(exampleFile, ".pslrc.example")
	if err != nil {
		t.Fatalf("Parse(.pslrc.example) error: %v", err)
	}
	if cfg.DefaultModel == "" {
		t.Error("the example sets no default_model")
	}
	if _, ok := cfg.Models[cfg.DefaultModel]; !ok {
		t.Errorf("default_model = %q, which has no section in the example", cfg.DefaultModel)
	}
}

func TestVersionString(t *testing.T) {
	released := strings.TrimSpace(versionFile)
	if released == "" {
		t.Fatal("the VERSION file is empty; it should hold the released version")
	}

	tests := []struct {
		name  string
		build string
		want  string
	}{
		{"no build stamp", "", "psl " + released},
		{"build stamp matches the release", "v" + released, "psl " + released},
		{"development build", "abc1234-dirty", "psl " + released + " (abc1234-dirty)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := version
			version = tc.build
			defer func() { version = original }()

			if got := versionString(); got != tc.want {
				t.Errorf("versionString() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrintReport(t *testing.T) {
	report := psllog.Report{
		Models: []psllog.Totals{
			{Model: "claude-opus-5", Requests: 12, InputTokens: 14203, OutputTokens: 1872, TotalTokens: 16075},
			{Model: "gpt-5.6", Requests: 3, InputTokens: 2110, OutputTokens: 405, TotalTokens: 2515},
		},
		Total: psllog.Totals{Requests: 15, InputTokens: 16313, OutputTokens: 2277, TotalTokens: 18590},
	}

	var out strings.Builder
	printReport(&out, report)
	want := strings.Join([]string{
		"MODEL          REQUESTS  INPUT  OUTPUT  TOTAL",
		"claude-opus-5        12  14203    1872  16075",
		"gpt-5.6               3   2110     405   2515",
		"TOTAL                15  16313    2277  18590",
		"",
	}, "\n")
	if out.String() != want {
		t.Errorf("printReport() =\n%s\nwant\n%s", out.String(), want)
	}
}

// One model's row is already the whole log; a total under it would say the
// same thing twice.
func TestPrintReportOmitsTheTotalForOneModel(t *testing.T) {
	report := psllog.Report{
		Models: []psllog.Totals{{Model: "m", Requests: 1, TotalTokens: 3}},
		Total:  psllog.Totals{Requests: 1, TotalTokens: 3},
	}
	var out strings.Builder
	printReport(&out, report)
	if lines := strings.Count(out.String(), "\n"); lines != 2 {
		t.Errorf("printReport() =\n%s\nwant a heading and one row", out.String())
	}
	// The heading has a TOTAL column of its own, so it is a row beginning with
	// the word that would be the summary line.
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "TOTAL") {
			t.Errorf("printReport() =\n%s\nwant no total row", out.String())
		}
	}
}

// A column of zeroes would be noise, so errors are only shown once there are
// some to show.
func TestPrintReportShowsErrorsOnlyWhenThereAreSome(t *testing.T) {
	clean := psllog.Report{
		Models: []psllog.Totals{{Model: "m", Requests: 2, TotalTokens: 3}},
		Total:  psllog.Totals{Requests: 2, TotalTokens: 3},
	}
	var out strings.Builder
	printReport(&out, clean)
	if strings.Contains(out.String(), "ERRORS") {
		t.Errorf("printReport() =\n%s\nwant no errors column", out.String())
	}

	failing := psllog.Report{
		Models: []psllog.Totals{{Model: "m", Requests: 2, Errors: 1, TotalTokens: 3}},
		Total:  psllog.Totals{Requests: 2, Errors: 1, TotalTokens: 3},
	}
	out.Reset()
	printReport(&out, failing)
	if !strings.Contains(out.String(), "ERRORS") {
		t.Errorf("printReport() =\n%s\nwant an errors column", out.String())
	}
}

func TestPeriod(t *testing.T) {
	first := time.Date(2026, 8, 10, 6, 18, 0, 0, time.UTC)
	last := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		total psllog.Totals
		want  string
	}{
		{"no times", psllog.Totals{}, ""},
		{"a span", psllog.Totals{First: first, Last: last}, " (2026-08-10 to 2026-08-13)"},
		{"one day", psllog.Totals{First: first, Last: first.Add(time.Hour)}, " (2026-08-10)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := period(tc.total); got != tc.want {
				t.Errorf("period() = %q, want %q", got, tc.want)
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
