package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"psl/internal/compiler"
	"psl/internal/lang"
	"psl/internal/slot"
)

// executionStep is one process in a language's run pipeline. Interpreted
// languages have one step; C and Rust first compile an ephemeral binary and
// then execute it.
type executionStep struct {
	path string
	args []string
}

type executionPlan struct {
	steps   []executionStep
	cleanup func()
}

// lookPath is a variable so the executor table can be tested without depending
// on which language toolchains happen to be installed on the test machine.
var lookPath = exec.LookPath

var execute = executePlan

// runCompiled translates path, writes the language file beside it without the
// trailing .psl, and runs that generated file. Python slots become runtime
// expressions; the other languages are resolved before execution. The input is
// never modified, and a failed translation leaves any previous output untouched.
func runCompiled(ctx context.Context, opts compiler.Options, programArgs []string, in io.Reader, out, errOut io.Writer) (int, error) {
	language, ext, err := lang.Of(opts.Path)
	if err != nil {
		return 1, err
	}
	if language == lang.Generic {
		return 1, fmt.Errorf("no executor for .%s files", ext)
	}
	generated := strings.TrimSuffix(opts.Path, filepath.Ext(opts.Path))
	plan, err := planExecution(ext, generated, programArgs)
	if err != nil {
		return 1, err
	}
	if plan.cleanup != nil {
		defer plan.cleanup()
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
	if ext == "py" || ext == "pyi" || ext == "pyw" {
		executable, err := os.Executable()
		if err != nil {
			return 1, fmt.Errorf("find the psl executable: %w", err)
		}
		var translated int
		source, translated, err = translatePythonRuntime(source, language, executable, opts)
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

// translatePythonRuntime turns each Python slot into a subprocess expression
// that asks this psl executable to resolve the slot when Python reaches it.
// A slot may stand on its own as an expression or occupy an entire string
// literal. The latter includes f-strings, so loop variables can be interpolated
// into the instruction independently on every iteration.
func translatePythonRuntime(source string, language *lang.Language, executable string, opts compiler.Options) (string, int, error) {
	sx := language.Analyze(source)
	slots := slot.All(source, language)
	for i := len(slots) - 1; i >= 0; i-- {
		s := slots[i]
		start, end := s.Start, s.End
		slotExpression := strconv.Quote(source[s.Start:s.End])

		if sx.InComment(s.Start) {
			return "", 0, fmt.Errorf("%s: runtime slot at %s is inside a comment; use it as a Python expression or as the whole contents of a string",
				opts.Path, sourcePosition(source, s.Start))
		}
		if stringStart, stringEnd, ok := sx.StringAt(s.Start); ok {
			literal := source[stringStart:stringEnd]
			only, bytes := pythonLiteralContainsOnlySlot(literal, s.Start-stringStart, s.End-stringStart)
			if !only {
				return "", 0, fmt.Errorf("%s: runtime slot at %s must occupy the entire Python string literal",
					opts.Path, sourcePosition(source, s.Start))
			}
			if bytes {
				return "", 0, fmt.Errorf("%s: runtime slot at %s cannot use a bytes literal",
					opts.Path, sourcePosition(source, s.Start))
			}
			start, end = stringStart, stringEnd
			// Passing the original literal expression preserves Python escapes and
			// lets an f-string interpolate the current loop/request values.
			slotExpression = literal
		}

		call := pythonRuntimeCall(executable, slotExpression, opts)
		source = source[:start] + call + source[end:]
	}
	return source, len(slots), nil
}

func pythonLiteralContainsOnlySlot(literal string, slotStart, slotEnd int) (only, bytes bool) {
	if slotStart < 0 || slotEnd < slotStart || slotEnd > len(literal) {
		return false, false
	}
	empty := literal[:slotStart] + literal[slotEnd:]
	i := 0
	for i < len(empty) && i < 2 && strings.ContainsRune("rRbBfFuU", rune(empty[i])) {
		i++
	}
	prefix, fences := empty[:i], empty[i:]
	switch fences {
	case `""`, `''`, `""""""`, `''''''`:
		return true, strings.ContainsAny(prefix, "bB")
	default:
		return false, false
	}
}

func pythonRuntimeCall(executable, slotExpression string, opts compiler.Options) string {
	parts := []string{strconv.Quote(executable), strconv.Quote("resolve"), slotExpression}
	if opts.Prompt != "" {
		parts = append(parts, strconv.Quote("--prompt"), strconv.Quote(opts.Prompt))
	}
	if opts.Image != nil {
		image := "data:" + opts.Image.MediaType + ";base64," + opts.Image.Base64
		parts = append(parts, strconv.Quote("--image"), strconv.Quote(image))
	}
	return `__import__("subprocess").check_output([` + strings.Join(parts, ", ") + `], text=True)`
}

func sourcePosition(source string, offset int) string {
	line, column := 1, 1
	for i := 0; i < offset && i < len(source); i++ {
		if source[i] == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return fmt.Sprintf("line %d, column %d", line, column)
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

// planExecution selects the conventional executor for the language named by
// ext. It never invokes a shell, so paths and program arguments are passed
// through verbatim.
func planExecution(ext, source string, programArgs []string) (executionPlan, error) {
	ext = strings.ToLower(ext)
	switch ext {
	case "py", "pyi", "pyw":
		path, prefix, err := findPython(source)
		if err != nil {
			return executionPlan{}, err
		}
		args := append(prefix, source)
		return oneStep(path, append(args, programArgs...)), nil

	case "js", "mjs", "cjs":
		path, _, err := findExecutor("JavaScript", "node")
		if err != nil {
			return executionPlan{}, err
		}
		return oneStep(path, append([]string{source}, programArgs...)), nil

	case "jsx", "ts", "tsx", "mts", "cts":
		path, name, err := findExecutor("TypeScript/JSX", "tsx", "bun", "deno")
		if err != nil {
			return executionPlan{}, err
		}
		args := []string{source}
		if name == "deno" || name == "bun" {
			args = []string{"run", source}
		}
		return oneStep(path, append(args, programArgs...)), nil

	case "go":
		path, _, err := findExecutor("Go", "go")
		if err != nil {
			return executionPlan{}, err
		}
		return oneStep(path, append([]string{"run", source}, programArgs...)), nil

	case "c", "h":
		cc, _, err := findExecutor("C", "cc", "clang", "gcc")
		if err != nil {
			return executionPlan{}, err
		}
		args := []string{source, "-o"}
		if ext == "h" {
			args = []string{"-x", "c", source, "-o"}
		}
		return compiledPlan(cc, args, programArgs)

	case "rs":
		rustc, _, err := findExecutor("Rust", "rustc")
		if err != nil {
			return executionPlan{}, err
		}
		return compiledPlan(rustc, []string{source, "-o"}, programArgs)

	case "cs":
		path, _, err := findExecutor("C#", "dotnet")
		if err != nil {
			return executionPlan{}, err
		}
		return oneStep(path, append([]string{"run", "--file", source, "--"}, programArgs...)), nil

	case "csx":
		path, _, err := findExecutor("C# script", "dotnet-script")
		if err != nil {
			return executionPlan{}, err
		}
		return oneStep(path, append([]string{source, "--"}, programArgs...)), nil

	default:
		return executionPlan{}, fmt.Errorf("no executor configured for .%s files", ext)
	}
}

// findPython respects an activated virtual environment first, then looks for
// the nearest project-local .venv beside the source or one of its parents.
// Falling back to PATH last avoids silently running a project's program under
// a system Python that cannot see the project's installed dependencies.
func findPython(source string) (path string, prefix []string, err error) {
	if active := os.Getenv("VIRTUAL_ENV"); active != "" {
		if path := environmentPython(active); executable(path) {
			return path, nil, nil
		}
	}

	abs, absErr := filepath.Abs(source)
	if absErr == nil {
		for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
			if path := environmentPython(filepath.Join(dir, ".venv")); executable(path) {
				return path, nil, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}

	candidates := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python", "py"}
	}
	path, name, err := findExecutor("Python", candidates...)
	if err != nil {
		return "", nil, err
	}
	if name == "py" {
		return path, []string{"-3"}, nil
	}
	return path, nil, nil
}

func environmentPython(environment string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(environment, "Scripts", "python.exe")
	}
	return filepath.Join(environment, "bin", "python")
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func oneStep(path string, args []string) executionPlan {
	return executionPlan{steps: []executionStep{{path: path, args: args}}}
}

func compiledPlan(compilerPath string, compileArgs, programArgs []string) (executionPlan, error) {
	dir, err := os.MkdirTemp("", "psl-run-*")
	if err != nil {
		return executionPlan{}, fmt.Errorf("create temporary executable directory: %w", err)
	}
	binary := filepath.Join(dir, "program")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	compileArgs = append(compileArgs, binary)
	return executionPlan{
		steps: []executionStep{
			{path: compilerPath, args: compileArgs},
			{path: binary, args: programArgs},
		},
		cleanup: func() { _ = os.RemoveAll(dir) },
	}, nil
}

func findExecutor(language string, candidates ...string) (path, name string, err error) {
	for _, candidate := range candidates {
		path, err := lookPath(candidate)
		if err == nil {
			return path, candidate, nil
		}
	}
	return "", "", fmt.Errorf("cannot run %s: install one of: %s", language, strings.Join(candidates, ", "))
}

func executePlan(ctx context.Context, plan executionPlan, in io.Reader, out, errOut io.Writer) (int, error) {
	for _, step := range plan.steps {
		cmd := exec.CommandContext(ctx, step.path, step.args...)
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
		return 1, fmt.Errorf("run %s: %w", step.path, err)
	}
	return 0, nil
}
