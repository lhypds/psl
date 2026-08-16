package macro

import (
	"fmt"

	"psl/internal/executor"
)

// ExecutionPlan reports that Macro PSL has no conventional external runtime.
func ExecutionPlan(ext, _ string, _ []string, _ executor.LookPath) (executor.Plan, error) {
	return executor.Plan{}, fmt.Errorf("no executor configured for .%s files", ext)
}
