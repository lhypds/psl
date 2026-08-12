package psllog

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// write appends raw lines to the log a Logger would have written, so a report
// can be read back from a file that is not necessarily well formed.
func write(t *testing.T, base string, lines ...string) *Logger {
	t.Helper()
	logger, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(logger.Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	return logger
}

func find(t *testing.T, report Report, model string) Totals {
	t.Helper()
	for _, totals := range report.Models {
		if totals.Model == model {
			return totals
		}
	}
	t.Fatalf("report has no totals for %q, got %+v", model, report.Models)
	return Totals{}
}

func TestSummarizeTotalsByModel(t *testing.T) {
	base := t.TempDir()
	logger, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	entries := []Entry{
		{Model: Model{Name: "claude-opus-5"}, Usage: &Usage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}},
		{Model: Model{Name: "claude-opus-5"}, Usage: &Usage{InputTokens: 200, OutputTokens: 30, TotalTokens: 230}},
		{Model: Model{Name: "gpt-5.6"}, Usage: &Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
	}
	for _, e := range entries {
		if err := logger.Log(e); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if report.Path != logger.Path() {
		t.Errorf("Path = %q, want %q", report.Path, logger.Path())
	}

	opus := find(t, report, "claude-opus-5")
	want := Totals{Model: "claude-opus-5", Requests: 2, InputTokens: 300, OutputTokens: 50, TotalTokens: 350}
	opus.First, opus.Last = time.Time{}, time.Time{}
	if opus != want {
		t.Errorf("claude-opus-5 = %+v, want %+v", opus, want)
	}

	total := report.Total
	total.First, total.Last = time.Time{}, time.Time{}
	wantTotal := Totals{Requests: 3, InputTokens: 310, OutputTokens: 55, TotalTokens: 365}
	if total != wantTotal {
		t.Errorf("Total = %+v, want %+v", total, wantTotal)
	}
	if report.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", report.Skipped)
	}
}

// The report is read to see which model spent the most, so that is the order.
func TestSummarizeOrdersModelsByTokensSpent(t *testing.T) {
	base := t.TempDir()
	logger, _ := Open(base)
	for _, e := range []Entry{
		{Model: Model{Name: "small"}, Usage: &Usage{TotalTokens: 10}},
		{Model: Model{Name: "big"}, Usage: &Usage{TotalTokens: 900}},
		{Model: Model{Name: "middle"}, Usage: &Usage{TotalTokens: 100}},
		// Two models that spent nothing sort by name, so the same log always
		// prints the same way.
		{Model: Model{Name: "zeta"}, Error: "boom"},
		{Model: Model{Name: "alpha"}, Error: "boom"},
	} {
		if err := logger.Log(e); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, totals := range report.Models {
		got = append(got, totals.Model)
	}
	want := []string{"big", "middle", "small", "alpha", "zeta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v", got, want)
	}
}

func TestSummarizeCountsFailedRequests(t *testing.T) {
	base := t.TempDir()
	logger, _ := Open(base)
	for _, e := range []Entry{
		{Model: Model{Name: "claude-opus-5"}, Usage: &Usage{InputTokens: 5, OutputTokens: 1, TotalTokens: 6}},
		{Model: Model{Name: "claude-opus-5"}, Error: "429 rate limited"},
	} {
		if err := logger.Log(e); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatal(err)
	}
	totals := find(t, report, "claude-opus-5")
	if totals.Requests != 2 || totals.Errors != 1 {
		t.Errorf("Requests, Errors = %d, %d; want 2, 1", totals.Requests, totals.Errors)
	}
	// A request that failed spent nothing, so it must not move the tokens.
	if totals.TotalTokens != 6 {
		t.Errorf("TotalTokens = %d, want the successful request's 6", totals.TotalTokens)
	}
}

// Some endpoints report the two halves and no sum.
func TestSummarizeAddsUpHalvesWhenNoTotalIsReported(t *testing.T) {
	base := t.TempDir()
	logger, _ := Open(base)
	if err := logger.Log(Entry{Model: Model{Name: "m"}, Usage: &Usage{InputTokens: 7, OutputTokens: 3}}); err != nil {
		t.Fatal(err)
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, report, "m").TotalTokens; got != 10 {
		t.Errorf("TotalTokens = %d, want the halves added up to 10", got)
	}
}

func TestSummarizeRecordsWhenEachModelWasUsed(t *testing.T) {
	base := t.TempDir()
	logger, _ := Open(base)
	first := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	last := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{last, first, first.Add(time.Hour)} {
		if err := logger.Log(Entry{Time: at, Model: Model{Name: "m"}}); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatal(err)
	}
	totals := find(t, report, "m")
	if !totals.First.Equal(first) || !totals.Last.Equal(last) {
		t.Errorf("First, Last = %v, %v; want %v, %v", totals.First, totals.Last, first, last)
	}
	if !report.Total.First.Equal(first) || !report.Total.Last.Equal(last) {
		t.Errorf("Total First, Last = %v, %v; want %v, %v", report.Total.First, report.Total.Last, first, last)
	}
}

// A run interrupted mid-write leaves a half line, which is worth stepping over
// and counting rather than failing the whole report for.
func TestSummarizeSkipsUnreadableLines(t *testing.T) {
	base := t.TempDir()
	write(t, base,
		`{"model":{"name":"m"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		`{"model":{"name":"m"`,
		"",
		"   ",
		`{"model":{"name":"m"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
	)

	report, err := Summarize(base)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if report.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 — blank lines are not entries and are not counted", report.Skipped)
	}
	if got := find(t, report, "m"); got.Requests != 2 || got.TotalTokens != 4 {
		t.Errorf("totals = %+v, want the two readable entries", got)
	}
}

// The last entry of a log psl is still appending to has no trailing newline.
func TestSummarizeReadsAnUnterminatedLastLine(t *testing.T) {
	base := t.TempDir()
	logger, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logger.Path(), []byte(`{"model":{"name":"m"},"usage":{"total_tokens":9}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, report, "m").TotalTokens; got != 9 {
		t.Errorf("TotalTokens = %d, want 9", got)
	}
}

// An entry holds the whole prompt, so lines run far past any fixed buffer.
func TestSummarizeReadsVeryLongLines(t *testing.T) {
	base := t.TempDir()
	logger, _ := Open(base)
	if err := logger.Log(Entry{
		Model: Model{Name: "m"},
		Slot:  Slot{Instruction: strings.Repeat("x", 1<<20)},
		Usage: &Usage{TotalTokens: 3},
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Summarize(base)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if got := find(t, report, "m").TotalTokens; got != 3 {
		t.Errorf("TotalTokens = %d, want 3", got)
	}
}

func TestSummarizeNamesAModelWithoutOne(t *testing.T) {
	base := t.TempDir()
	write(t, base, `{"usage":{"total_tokens":4}}`)

	report, err := Summarize(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := find(t, report, UnknownModel); got.TotalTokens != 4 {
		t.Errorf("totals = %+v, want the entry filed under %q", got, UnknownModel)
	}
}

// Nothing logged yet is an empty history, not a broken one.
func TestSummarizeWithoutALog(t *testing.T) {
	base := t.TempDir()
	report, err := Summarize(base)
	if !errors.Is(err, ErrNoLog) {
		t.Fatalf("Summarize() error = %v, want ErrNoLog", err)
	}
	if report.Path == "" {
		t.Error("Path is empty, want the log that would have been read")
	}
	if len(report.Models) != 0 {
		t.Errorf("Models = %+v, want none", report.Models)
	}
}

func TestSummarizeReportsAnUnreadableLog(t *testing.T) {
	base := t.TempDir()
	logger, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the log file belongs cannot be read as one.
	if err := os.Mkdir(logger.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Summarize(base); err == nil || errors.Is(err, ErrNoLog) {
		t.Fatalf("Summarize() error = %v, want a read failure", err)
	}
}
