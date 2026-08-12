package psllog

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ErrNoLog reports that there is nothing to summarize: the log file appears the
// first time psl resolves a slot, so its absence is an empty history rather
// than a failure.
var ErrNoLog = errors.New("no log file")

// UnknownModel stands in for an entry whose model has no name, which an entry
// written by a future version of psl could have.
const UnknownModel = "(unknown)"

// Totals is what one model spent.
type Totals struct {
	Model        string
	Requests     int
	Errors       int // requests that came back as an error, and so spent nothing
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	First        time.Time // when this model was first and last used; zero if
	Last         time.Time // no entry carried a time
}

// Report is the log summarized by model.
type Report struct {
	Path   string
	Models []Totals // heaviest first
	Total  Totals   // every model together, with Model left empty
	// Skipped counts lines that were not readable entries, so a report that
	// misses requests can say so rather than quietly undercount.
	Skipped int
}

// Summarize totals the tokens each model spent, from baseDir/.psl/psl.log. psl
// passes the user's home directory, the same one it logs to.
func Summarize(baseDir string) (Report, error) {
	path := filepath.Join(baseDir, Dir, File)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Report{Path: path}, fmt.Errorf("%s: %w", path, ErrNoLog)
	}
	if err != nil {
		return Report{Path: path}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	report, err := summarize(f)
	report.Path = path
	if err != nil {
		return report, fmt.Errorf("read %s: %w", path, err)
	}
	return report, nil
}

// logLine is the part of an Entry a report needs. Decoding into this rather
// than into Entry leaves the request body — by far the largest field, since it
// holds the whole prompt — unread instead of in memory.
type logLine struct {
	Time  time.Time `json:"time"`
	Model struct {
		Name string `json:"name"`
	} `json:"model"`
	Usage *Usage `json:"usage"`
	Error string `json:"error"`
}

// summarize reads one JSON entry per line and adds each to its model's totals.
func summarize(r io.Reader) (Report, error) {
	var report Report
	byModel := make(map[string]*Totals)

	// A line carries a whole prompt and response, so it is read with a reader
	// that grows to fit rather than with a bufio.Scanner, which gives up on a
	// line past a fixed size.
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var e logLine
			if json.Unmarshal(line, &e) != nil {
				// A line half-written by an interrupted run is worth counting
				// and stepping over, not worth failing the whole report for.
				report.Skipped++
			} else {
				name := cmp.Or(e.Model.Name, UnknownModel)
				totals, ok := byModel[name]
				if !ok {
					totals = &Totals{Model: name}
					byModel[name] = totals
				}
				totals.add(e)
				report.Total.add(e)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return report, err
		}
	}

	for _, totals := range byModel {
		report.Models = append(report.Models, *totals)
	}
	// Heaviest first: what a report of what was spent is read for is which
	// model spent it. Models that spent the same are ordered by name, so the
	// same log always prints the same way.
	slices.SortFunc(report.Models, func(a, b Totals) int {
		return cmp.Or(
			cmp.Compare(b.TotalTokens, a.TotalTokens),
			cmp.Compare(b.Requests, a.Requests),
			cmp.Compare(a.Model, b.Model),
		)
	})
	return report, nil
}

// add folds one entry into the totals.
func (t *Totals) add(e logLine) {
	t.Requests++
	if e.Error != "" {
		t.Errors++
	}
	if e.Usage != nil {
		t.InputTokens += e.Usage.InputTokens
		t.OutputTokens += e.Usage.OutputTokens
		t.TotalTokens += e.Usage.total()
	}
	if !e.Time.IsZero() {
		if t.First.IsZero() || e.Time.Before(t.First) {
			t.First = e.Time
		}
		if e.Time.After(t.Last) {
			t.Last = e.Time
		}
	}
}

// total is what one request cost altogether. An endpoint that reports the two
// halves but not their sum is common enough to add them up here, rather than
// report a model that spent nothing.
func (u Usage) total() int {
	if u.TotalTokens != 0 {
		return u.TotalTokens
	}
	return u.InputTokens + u.OutputTokens
}
