package macro

import (
	"strings"
	"testing"
)

func TestExecutionPlanReportsNoExecutor(t *testing.T) {
	if _, err := ExecutionPlan("macro", "main.macro", nil, nil); err == nil || !strings.Contains(err.Error(), ".macro") {
		t.Fatalf("ExecutionPlan() error = %v, want an unsupported-executor error", err)
	}
}
