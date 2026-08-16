package typescript

import "psl/internal/executor"

// ExecutionPlan runs TypeScript or JSX with the first installed direct
// runtime. None of the candidates invokes a package downloader.
func ExecutionPlan(_ string, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error) {
	path, name, err := executor.Find("TypeScript/JSX", lookPath, "tsx", "bun", "deno")
	if err != nil {
		return executor.Plan{}, err
	}
	args := []string{source}
	if name == "deno" || name == "bun" {
		args = []string{"run", source}
	}
	return executor.OneStep(path, append(args, programArgs...)), nil
}
