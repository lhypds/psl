// Package executor holds the language-independent mechanics behind psl run:
// executable discovery, process plans, temporary native binaries, standard IO,
// and child exit codes. Each language folder decides which plan it needs.
package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LookPath is the executable lookup operation accepted by language plans.
// Taking it as an argument keeps those plans deterministic in tests.
type LookPath func(string) (string, error)

// Step is one process in a language's execution pipeline.
type Step struct {
	Path string
	Args []string
}

// Plan is the process pipeline for a generated language file. Interpreted
// languages normally have one step; native languages compile and then run.
type Plan struct {
	Steps   []Step
	Cleanup func()
}

// Find returns the first available executable from candidates.
func Find(language string, lookPath LookPath, candidates ...string) (path, name string, err error) {
	for _, candidate := range candidates {
		path, err := lookPath(candidate)
		if err == nil {
			return path, candidate, nil
		}
	}
	return "", "", fmt.Errorf("cannot run %s: install one of: %s", language, strings.Join(candidates, ", "))
}

// OneStep makes an interpreted-language plan.
func OneStep(path string, args []string) Plan {
	return Plan{Steps: []Step{{Path: path, Args: args}}}
}

// Compiled makes a native-language plan whose compiler accepts -o followed by
// an output path at the end of compileArgs. The binary is temporary; the
// generated source remains beside the PSL input.
func Compiled(compilerPath string, compileArgs, programArgs []string) (Plan, error) {
	dir, err := os.MkdirTemp("", "psl-run-*")
	if err != nil {
		return Plan{}, fmt.Errorf("create temporary executable directory: %w", err)
	}
	binary := filepath.Join(dir, "program")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	compileArgs = append(compileArgs, binary)
	return Plan{
		Steps: []Step{
			{Path: compilerPath, Args: compileArgs},
			{Path: binary, Args: programArgs},
		},
		Cleanup: func() { _ = os.RemoveAll(dir) },
	}, nil
}

// Execute runs a plan with inherited-style standard IO and returns the first
// non-zero child exit code without wrapping it as a psl error.
func Execute(ctx context.Context, plan Plan, in io.Reader, out, errOut io.Writer) (int, error) {
	for _, step := range plan.Steps {
		cmd := exec.CommandContext(ctx, step.Path, step.Args...)
		cmd.Stdin = in
		cmd.Stdout = out
		cmd.Stderr = errOut
		err := cmd.Run()
		if err == nil {
			continue
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if code := exitErr.ExitCode(); code >= 0 {
				return code, nil
			}
			if ctx.Err() != nil {
				return 130, nil
			}
			return 1, nil
		}
		return 1, fmt.Errorf("run %s: %w", step.Path, err)
	}
	return 0, nil
}
