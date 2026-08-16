package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"psl/internal/compiler"
	"psl/internal/executor"
	"psl/internal/lang"
	"psl/internal/slot"
)

// lookPath is a variable so the executor table can be tested without depending
// on which language toolchains happen to be installed on the test machine.
var lookPath executor.LookPath = exec.LookPath

var execute = executor.Execute

// runCompiled translates path, writes the language file beside it without the
// trailing .psl, and runs that generated file. A language may translate slots
// into runtime expressions; otherwise they are resolved before execution. The
// input is never modified, and a failed translation leaves prior output intact.
func runCompiled(ctx context.Context, opts compiler.Options, programArgs []string, in io.Reader, out, errOut io.Writer) (int, error) {
	language, ext, err := lang.Of(opts.Path)
	if err != nil {
		return 1, err
	}
	if language == lang.Generic {
		return 1, fmt.Errorf("no executor for .%s files", ext)
	}
	if language.ExecutionPlan == nil {
		return 1, fmt.Errorf("no executor configured for .%s files", ext)
	}
	generated := strings.TrimSuffix(opts.Path, filepath.Ext(opts.Path))
	plan, err := language.ExecutionPlan(ext, generated, programArgs, lookPath)
	if err != nil {
		return 1, err
	}
	if plan.Cleanup != nil {
		defer plan.Cleanup()
	}

	info, err := os.Stat(opts.Path)
	if err != nil {
		return 1, err
	}
	if info.IsDir() {
		return 1, fmt.Errorf("%s is a directory", opts.Path)
	}
	data, err := os.ReadFile(opts.Path)
	if err != nil {
		return 1, err
	}

	source := string(data)
	if language.TranslateRuntime != nil {
		executable, err := os.Executable()
		if err != nil {
			return 1, fmt.Errorf("find the psl executable: %w", err)
		}
		sourcePath, err := filepath.Abs(opts.Path)
		if err != nil {
			return 1, fmt.Errorf("resolve source path %s: %w", opts.Path, err)
		}
		runtimeOpts := lang.RuntimeOptions{
			Path:       opts.Path,
			SourcePath: sourcePath,
			Executable: executable,
			Prompt:     opts.Prompt,
		}
		if opts.Image != nil {
			runtimeOpts.ImageMediaType = opts.Image.MediaType
			runtimeOpts.ImageBase64 = opts.Image.Base64
		}
		found := slot.All(source, language)
		runtimeSlots := make([]lang.RuntimeSlot, len(found))
		for i, s := range found {
			runtimeSlots[i] = lang.RuntimeSlot{Start: s.Start, End: s.End}
		}
		var translated int
		source, translated, err = language.TranslateRuntime(source, language.Analyze(source), runtimeSlots, runtimeOpts)
		if err != nil {
			return 1, err
		}
		if translated > 0 {
			fmt.Fprintf(errOut, "psl: %s translated %d runtime slot(s)\n", opts.Path, translated)
		}
	} else {
		remaining := slot.Count(source, language)
		for remaining > 0 {
			result, err := compiler.CompileSource(ctx, source, opts)
			if err != nil {
				return 1, err
			}
			// A replacement is forbidden from manufacturing another PSL slot. Apart
			// from avoiding an accidental unbounded series of paid requests, this
			// keeps run scoped to the instructions the author actually wrote.
			if result.Remaining >= remaining {
				return 1, fmt.Errorf("model %s introduced a new AI slot while resolving %s",
					result.Model, summarize(result.Instruction))
			}
			source = result.Source
			remaining = result.Remaining
			printResolved(errOut, opts.Path, result)
		}
	}

	if err := writeGenerated(generated, source, info.Mode().Perm()); err != nil {
		return 1, err
	}
	fmt.Fprintf(errOut, "psl: wrote %s\n", generated)

	return execute(ctx, plan, in, out, errOut)
}

// writeGenerated atomically replaces the generated language file, so an
// interrupted write cannot leave behind half a program.
func writeGenerated(path, source string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".psl-run-*")
	if err != nil {
		return fmt.Errorf("create temporary output in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(source); err != nil {
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
