package python

import (
	"os"
	"path/filepath"
	"runtime"

	"psl/internal/executor"
)

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
