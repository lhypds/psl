package c

import "psl/internal/executor"

// ExecutionPlan compiles a C source (or an explicitly C header) to a temporary
// native binary and runs it.
func ExecutionPlan(ext, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	cc, _, err := executor.Find("C", lookPath, "cc", "clang", "gcc")
	if err != nil {
		return executor.Plan{}, err
	}
	args := []string{source, "-o"}
	if ext == "h" {
		args = []string{"-x", "c", source, "-o"}
	}
	return executor.Compiled(cc, args, programArgs)
}
