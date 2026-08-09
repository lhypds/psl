// Package compiler resolves one PSL slot per run and writes the result back
// into the source file.
package compiler

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Log     *psllog.Logger // request log; nil records nothing
	Version string         // psl version, recorded in the log

	// NewClient builds the API client for a model. Defaults to llm.New.
	NewClient func(*pslrc.Model) (llm.Client, error)
}

// Result describes the slot that was resolved.
type Result struct {
	Model       string
	Instruction string
	Replacement string
	Remaining   int // slots still unresolved after this run
	Usage       llm.Usage
}

// Compile resolves the first slot in opts.Path. The file is left untouched
// unless the model returns output, so a failed run can simply be retried.
func Compile(ctx context.Context, opts Options) (*Result, error) {
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

	s, ok := slot.Find(src)
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
	client, err := newClient(model)
	if err != nil {
		return nil, err
	}

	req := llm.Request{
		Model:     model.ID(),
		System:    systemPrompt,
		Prompt:    buildPrompt(filepath.Base(opts.Path), slot.Mask(src, s), s, opts.Image != nil),
		Image:     opts.Image,
		MaxTokens: model.MaxTokens,
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
		Instruction: s.Instruction,
		Replacement: replacement,
		Remaining:   slot.Count(compiled),
		Usage:       out.Usage,
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
	entry := psllog.Entry{
		PSLVersion: o.Version,
		File:       o.Path,
		Slot: psllog.Slot{
			Line:        line,
			Column:      column,
			Instruction: s.Instruction,
		},
		Model: psllog.Model{
			Name:      model.Name,
			ID:        model.ID(),
			BaseURL:   model.BaseURL,
			API:       string(model.Protocol()),
			Endpoint:  llm.Endpoint(model),
			MaxTokens: model.MaxTokens,
		},
		Request: psllog.Request{
			System: req.System,
			Prompt: req.Prompt,
		},
		DurationMS: took.Milliseconds(),
	}
	if req.Image != nil {
		// The base64 payload is deliberately left out; only its shape is useful.
		entry.Request.Image = &psllog.Image{
			MediaType: req.Image.MediaType,
			Bytes:     base64Size(req.Image.Base64),
		}
	}
	if callErr != nil {
		entry.Error = callErr.Error()
	}
	if out != nil {
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

// base64Size is the number of bytes a base64 payload decodes to. DecodedLen
// alone only bounds it, since it cannot see the padding.
func base64Size(payload string) int {
	return base64.StdEncoding.DecodedLen(len(payload)) - strings.Count(payload, "=")
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
