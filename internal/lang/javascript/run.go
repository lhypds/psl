package javascript

import "psl/internal/executor"

// ExecutionPlan runs JavaScript with Node.js.
func ExecutionPlan(_ string, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	path, _, err := executor.Find("JavaScript", lookPath, "node")
	if err != nil {
		return executor.Plan{}, err
	}
	return executor.OneStep(path, append([]string{source}, programArgs...)), nil
}
