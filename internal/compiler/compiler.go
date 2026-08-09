// Package compiler resolves one PSL slot per run and writes the result back
// into the source file.
package compiler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"psl/internal/llm"
	"psl/internal/pslrc"
	"psl/internal/slot"
)

// ErrNoSlots reports that the file holds no unresolved slot, which is how a
// fully compiled file looks.
var ErrNoSlots = errors.New("no AI slots left")

// Options configures one compilation run.
type Options struct {
	Path   string // file to compile, rewritten in place
	Config *pslrc.Config
	Image  *llm.Image // optional visual context for this run

	// NewClient builds the API client for a model. Defaults to llm.New.
	NewClient func(*pslrc.Model) (llm.Client, error)
}

// Result describes the slot that was resolved.
type Result struct {
	Model       string
	Instruction string
	Replacement string
	Remaining   int // slots still unresolved after this run
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

	out, err := client.Complete(ctx, llm.Request{
		Model:     model.ID(),
		System:    systemPrompt,
		Prompt:    buildPrompt(filepath.Base(opts.Path), slot.Mask(src, s), s, opts.Image != nil),
		Image:     opts.Image,
		MaxTokens: model.MaxTokens,
	})
	if err != nil {
		return nil, err
	}
	replacement := clean(out)
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
	}, nil
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

// position renders a byte offset as line:column for error messages.
func position(src string, offset int) string {
	line, col := 1, 1
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return fmt.Sprintf("line %d, column %d", line, col)
}
