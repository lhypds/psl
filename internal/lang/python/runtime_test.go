package python

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"psl/internal/executor"
	"psl/internal/lang"
	"psl/internal/slot"
)

func fakeLookPath(name string) (string, error) { return "/tools/" + name, nil }

func TestExecutionPlanSelectsPythonAndPassesArguments(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	plan, err := ExecutionPlan("py", "app.py", []string{"--name", "Ada"}, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []executor.Step{{Path: "/tools/python3", Args: []string{"app.py", "--name", "Ada"}}}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("steps = %#v, want %#v", plan.Steps, want)
	}
}

func TestExecutionPlanUsesTheProjectVirtualEnvironment(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	dir := t.TempDir()
	python := environmentPython(filepath.Join(dir, ".venv"))
	writeInterpreter(t, python)

	source := filepath.Join(dir, "src", "app.py")
	plan, err := ExecutionPlan("py", source, nil, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Path != python {
		t.Errorf("steps = %#v, want project interpreter %s", plan.Steps, python)
	}
}

func TestExecutionPlanPrefersTheActiveVirtualEnvironment(t *testing.T) {
	dir := t.TempDir()
	python := environmentPython(dir)
	writeInterpreter(t, python)
	t.Setenv("VIRTUAL_ENV", dir)

	plan, err := ExecutionPlan("py", filepath.Join(t.TempDir(), "app.py"), nil, fakeLookPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Path != python {
		t.Errorf("steps = %#v, want active interpreter %s", plan.Steps, python)
	}
}

func writeInterpreter(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test interpreter"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestTranslateRuntimeEvaluatesAnFStringAtEachCallSite(t *testing.T) {
	const source = "for i in range(2):\n    print(f\":: give me value number {i} ::\")\n"
	translated, count, err := translate(source, lang.RuntimeOptions{
		Executable: "/usr/local/bin/psl",
		SourcePath: "/work/app.py.psl",
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("translated slots = %d, want 1", count)
	}
	want := `print(__import__("subprocess").check_output(["/usr/local/bin/psl", "resolve", f":: give me value number {i} ::", "--context-file", "/work/app.py.psl", "--context-offset", "31"], text=True))`
	if !strings.Contains(translated, want) {
		t.Errorf("translated source = %q, want runtime f-string call %q", translated, want)
	}
}

func TestTranslateRuntimeRejectsPartialStringsAndComments(t *testing.T) {
	for name, source := range map[string]string{
		"partial string": `message = "news: :: today in Tokyo ::"`,
		"comment":        `# :: write the loop ::`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := translate(source, lang.RuntimeOptions{Path: "app.py.psl", Executable: "psl"}); err == nil {
				t.Fatal("translation succeeded, want a runtime-slot placement error")
			}
		})
	}
}

func translate(source string, opts lang.RuntimeOptions) (string, int, error) {
	found := slot.All(source, Language)
	slots := make([]lang.RuntimeSlot, len(found))
	for i, s := range found {
		slots[i] = lang.RuntimeSlot{Start: s.Start, End: s.End}
	}
	return TranslateRuntime(source, Language.Analyze(source), slots, opts)
}
