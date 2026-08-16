package golang

import "psl/internal/executor"

// ExecutionPlan runs a generated Go source file with go run.
func ExecutionPlan(_ string, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	path, _, err := executor.Find("Go", lookPath, "go")
	if err != nil {
		return executor.Plan{}, err
	}
	return executor.OneStep(path, append([]string{"run", source}, programArgs...)), nil
}
