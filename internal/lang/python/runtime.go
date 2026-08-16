package python

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"psl/internal/executor"
	"psl/internal/lang"
	"psl/internal/slot"
)

// TranslateRuntime turns each Python slot into a subprocess expression that
// asks the psl executable to resolve it when Python reaches it. A slot may
// stand on its own as an expression or occupy an entire string literal. The
// latter includes f-strings, so current loop or request values can become part
// of every independently resolved instruction.
func TranslateRuntime(source string, opts lang.RuntimeOptions) (string, int, error) {
	sx := Language.Analyze(source)
	slots := slot.All(source, Language)
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
			only, bytes := literalContainsOnlySlot(literal, s.Start-stringStart, s.End-stringStart)
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
			// lets an f-string interpolate its current values.
			slotExpression = literal
		}

		call := runtimeCall(slotExpression, opts)
		source = source[:start] + call + source[end:]
	}
	return source, len(slots), nil
}

func literalContainsOnlySlot(literal string, slotStart, slotEnd int) (only, bytes bool) {
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

func runtimeCall(slotExpression string, opts lang.RuntimeOptions) string {
	parts := []string{strconv.Quote(opts.Executable), strconv.Quote("resolve"), slotExpression}
	if opts.Prompt != "" {
		parts = append(parts, strconv.Quote("--prompt"), strconv.Quote(opts.Prompt))
	}
	if opts.ImageMediaType != "" {
		image := "data:" + opts.ImageMediaType + ";base64," + opts.ImageBase64
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

// ExecutionPlan chooses the Python belonging to the active environment or
// source project before falling back to a system interpreter.
func ExecutionPlan(_ string, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	path, prefix, err := findInterpreter(source, lookPath)
	if err != nil {
		return executor.Plan{}, err
	}
	args := append(prefix, source)
	return executor.OneStep(path, append(args, programArgs...)), nil
}

func findInterpreter(source string, lookPath executor.LookPath) (path string, prefix []string, err error) {
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
	path, name, err := executor.Find("Python", lookPath, candidates...)
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
