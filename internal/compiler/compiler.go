// Package compiler resolves one PSL slot per run and writes the result back
// into the source file.
package compiler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"psl/internal/lang"
	"psl/internal/llm"
	"psl/internal/psllog"
	"psl/internal/pslrc"
	"psl/internal/slot"
)

// ErrNoSlots reports that the file holds no unresolved slot, which is how a
// fully compiled file looks.
var ErrNoSlots = errors.New("no AI slots left")

// Options configures one compilation run.
type Options struct {
	Path    string // file to compile, rewritten in place
	Config  *pslrc.Config
	Image   *llm.Image     // optional visual context for this run
	Prompt  string         // optional guidance from --prompt, added to the system prompt
	Log     *psllog.Logger // request log; nil records nothing
	Version string         // psl version, recorded in the log

	// NewClient builds the API client for a model. Defaults to llm.New.
	NewClient func(*pslrc.Model) llm.Client
	// NewSearcher builds the searcher answering web_search calls, for a model
	// whose section turned search on. Defaults to llm.NewSearcher.
	NewSearcher func(*pslrc.Model) llm.Searcher
}

// Result describes the slot that was resolved.
type Result struct {
	Model       string
	Language    string // language the source was parsed under
	Instruction string
	Replacement string
	Remaining   int // slots still unresolved after this run
	Usage       llm.Usage
	// Searches are the web searches the model made to resolve the slot, in the
	// order it asked them. Empty unless the model's section turned search on.
	Searches []llm.SearchResult
}

// Compile resolves the first slot in opts.Path. The file is left untouched
// unless the model returns output, so a failed run can simply be retried.
func Compile(ctx context.Context, opts Options) (*Result, error) {
	// Which `::` are slots depends on the language, and the name is what says
	// which language it is, so a misnamed file cannot be parsed at all. This
	// comes before the file is even opened: nothing else psl might complain
	// about is worth hearing until the name is right.
	language, ext, err := lang.Of(opts.Path)
	if err != nil {
		return nil, err
	}
	if language == lang.Generic {
		fmt.Fprintf(os.Stderr, "psl: warning: no rules for .%s, using the generic rules\n", ext)
	}

	info, err := os.Stat(opts.Path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", opts.Path)
	}
	source, err := os.ReadFile(opts.Path)
	if err != nil {
		return nil, err
	}
	src := string(source)

	s, ok := slot.Find(src, language)
	if !ok {
		return nil, ErrNoSlots
	}
	if s.Instruction == "" {
		return nil, fmt.Errorf("%s: empty slot at %s", opts.Path, position(src, s.Start))
	}

	model, err := opts.Config.Resolve(s.Model)
	if err != nil {
		return nil, err
	}
	newClient := opts.NewClient
	if newClient == nil {
		newClient = llm.New
	}
	client := newClient(model)

	// A model that cannot reach the web is not an error to find out about after
	// the slot has been paid for, so the search endpoint is resolved up front
	// with the model's own.
	searchModel, err := opts.Config.ResolveSearch(model)
	if err != nil {
		return nil, err
	}
	var search llm.Searcher
	if searchModel != nil {
		newSearcher := opts.NewSearcher
		if newSearcher == nil {
			newSearcher = llm.NewSearcher
		}
		search = newSearcher(searchModel)
	}

	req := llm.Request{
		Model:     model.Name,
		System:    buildSystem(filepath.Base(opts.Path), language, s, opts.Prompt, opts.Image != nil, search != nil),
		Prompt:    slot.Mask(src, s),
		Image:     opts.Image,
		MaxTokens: model.MaxTokens,
		Params:    model.Params,
		Search:    search,
	}

	started := time.Now()
	out, err := client.Complete(ctx, req)
	opts.record(src, s, model, req, out, time.Since(started), err)
	if err != nil {
		return nil, err
	}
	replacement := clean(out.Text)
	if replacement == "" {
		return nil, fmt.Errorf("model %s returned no usable text for the slot at %s",
			model.Name, position(src, s.Start))
	}

	compiled := slot.Replace(src, s, replacement)
	if err := writeFile(opts.Path, compiled, info.Mode().Perm()); err != nil {
		return nil, err
	}
	return &Result{
		Model:       model.Name,
		Language:    language.Name,
		Instruction: s.Instruction,
		Replacement: replacement,
		Remaining:   slot.Count(compiled, language),
		Usage:       out.Usage,
		Searches:    out.Searches,
	}, nil
}

// record writes one request to the log. Logging never fails a compilation: the
// request has already been paid for, so a log that cannot be written is only
// worth a warning.
func (o Options) record(src string, s slot.Slot, model *pslrc.Model, req llm.Request, out *llm.Response, took time.Duration, callErr error) {
	if o.Log == nil {
		return
	}
	line, column := lineColumn(src, s.Start)
	// One log holds every project's requests, so record where the file is, not
	// just what it was called on the command line.
	file := o.Path
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	body, err := llm.Body(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: warning: %v\n", err)
	}
	entry := psllog.Entry{
		PSLVersion: o.Version,
		File:       file,
		Slot: psllog.Slot{
			Line:        line,
			Column:      column,
			Instruction: s.Instruction,
		},
		Model: psllog.Model{
			Name:      model.Name,
			BaseURL:   model.BaseURL,
			Endpoint:  llm.Endpoint(model),
			MaxTokens: model.MaxTokens,
			WebSearch: model.WebSearch,
		},
		Request:    body,
		DurationMS: took.Milliseconds(),
	}
	if callErr != nil {
		entry.Error = callErr.Error()
	}
	if out != nil {
		entry.Searches = searchEntries(out.Searches)
		entry.Response = &psllog.Response{Text: out.Text, StopReason: out.StopReason}
		entry.Usage = &psllog.Usage{
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
			TotalTokens:  out.Usage.TotalTokens,
		}
	}
	if err := o.Log.Log(entry); err != nil {
		fmt.Fprintf(os.Stderr, "psl: warning: %v\n", err)
	}
}

// searchEntries renders the searches for the log. A source is kept as its URL
// alone: the title is the search model's phrasing of a page psl did not read,
// and the URL is the part that can be checked later.
func searchEntries(searches []llm.SearchResult) []psllog.Search {
	if len(searches) == 0 {
		return nil
	}
	entries := make([]psllog.Search, 0, len(searches))
	for _, s := range searches {
		entry := psllog.Search{Query: s.Query, Answer: s.Answer, Error: s.Error}
		for _, source := range s.Sources {
			entry.Sources = append(entry.Sources, source.URL)
		}
		entries = append(entries, entry)
	}
	return entries
}

// writeFile replaces path's contents atomically, so an interrupted write can
// never truncate the user's source file.
func writeFile(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".psl-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// lineColumn resolves a byte offset to a 1-based line and column.
func lineColumn(src string, offset int) (line, column int) {
	line, column = 1, 1
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

// position renders a byte offset as line:column for error messages.
func position(src string, offset int) string {
	line, column := lineColumn(src, offset)
	return fmt.Sprintf("line %d, column %d", line, column)
}
