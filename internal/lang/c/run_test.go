package c

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExecutionPlanCompilesBeforeRunning(t *testing.T) {
	plan, err := ExecutionPlan("c", "main.c", []string{"one"}, func(name string) (string, error) {
		return "/tools/" + name, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Cleanup == nil || len(plan.Steps) != 2 {
		t.Fatalf("plan = %#v, want a compile step, a run step, and cleanup", plan)
	}
	if got := plan.Steps[0]; got.Path != "/tools/cc" || !reflect.DeepEqual(got.Args[:2], []string{"main.c", "-o"}) {
		t.Errorf("compile step = %#v", got)
	}
	if got := plan.Steps[1]; got.Path != plan.Steps[0].Args[2] || !reflect.DeepEqual(got.Args, []string{"one"}) {
		t.Errorf("run step = %#v", got)
	}
	tempDir := filepath.Dir(plan.Steps[1].Path)
	plan.Cleanup()
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary directory still exists after cleanup: %v", err)
	}
}
