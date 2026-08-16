package rust

import "psl/internal/executor"

// ExecutionPlan compiles Rust to a temporary native binary and runs it.
func ExecutionPlan(_ string, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	rustc, _, err := executor.Find("Rust", lookPath, "rustc")
	if err != nil {
		return executor.Plan{}, err
	}
	return executor.Compiled(rustc, []string{source, "-o"}, programArgs)
}
