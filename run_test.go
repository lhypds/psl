package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"psl/internal/compiler"
	"psl/internal/lang"
)

func withFakeExecution(t *testing.T, run func(context.Context, executionPlan, io.Reader, io.Writer, io.Writer) (int, error)) {
	t.Helper()
	originalLookPath, originalExecute := lookPath, execute
	lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	execute = run
	t.Cleanup(func() {
		lookPath = originalLookPath
		execute = originalExecute
	})
}

func TestPlanExecutionSelectsExecutorAndPassesArguments(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	withFakeExecution(t, executePlan)

	plan, err := planExecution("py", "app.py", []string{"--name", "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	want := []executionStep{{path: "/tools/python3", args: []string{"app.py", "--name", "Ada"}}}
	if !reflect.DeepEqual(plan.steps, want) {
		t.Errorf("steps = %#v, want %#v", plan.steps, want)
	}
}

func TestPlanExecutionUsesTheProjectVirtualEnvironment(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	dir := t.TempDir()
	python := environmentPython(filepath.Join(dir, ".venv"))
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("test interpreter"), 0o755); err != nil {
		t.Fatal(err)
	}

	withFakeExecution(t, executePlan)
	source := filepath.Join(dir, "src", "app.py")
	plan, err := planExecution("py", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.steps) != 1 || plan.steps[0].path != python {
		t.Errorf("steps = %#v, want project interpreter %s", plan.steps, python)
	}
}

func TestPlanExecutionPrefersTheActiveVirtualEnvironment(t *testing.T) {
	dir := t.TempDir()
	python := environmentPython(dir)
	if err := os.MkdirAll(filepath.Dir(python), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("test interpreter"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIRTUAL_ENV", dir)

	withFakeExecution(t, executePlan)
	plan, err := planExecution("py", filepath.Join(t.TempDir(), "app.py"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.steps) != 1 || plan.steps[0].path != python {
		t.Errorf("steps = %#v, want active interpreter %s", plan.steps, python)
	}
}

func TestPlanExecutionCompilesCBeforeRunningIt(t *testing.T) {
	withFakeExecution(t, executePlan)

	plan, err := planExecution("c", "main.c", []string{"one"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.cleanup == nil || len(plan.steps) != 2 {
		t.Fatalf("plan = %#v, want a compile step, a run step, and cleanup", plan)
	}
	if got := plan.steps[0]; got.path != "/tools/cc" || !reflect.DeepEqual(got.args[:2], []string{"main.c", "-o"}) {
		t.Errorf("compile step = %#v", got)
	}
	if got := plan.steps[1]; got.path != plan.steps[0].args[2] || !reflect.DeepEqual(got.args, []string{"one"}) {
		t.Errorf("run step = %#v", got)
	}
	tempDir := filepath.Dir(plan.steps[1].path)
	plan.cleanup()
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary directory still exists after cleanup: %v", err)
	}
}

func TestPlanExecutionRejectsLanguagesWithoutAnExecutor(t *testing.T) {
	if _, err := planExecution("macro", "main.macro", nil); err == nil || !strings.Contains(err.Error(), ".macro") {
		t.Fatalf("planExecution() error = %v, want an unsupported-executor error", err)
	}
}

func TestRunCompiledWritesTheLanguageFileAndRunsIt(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "hello.py.psl")
	const source = "print('hello')\n"
	if err := os.WriteFile(input, []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}

	var gotPlan executionPlan
	withFakeExecution(t, func(_ context.Context, plan executionPlan, _ io.Reader, _, _ io.Writer) (int, error) {
		gotPlan = plan
		return 23, nil
	})

	code, err := runCompiled(context.Background(), compiler.Options{Path: input}, []string{"world"}, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if code != 23 {
		t.Errorf("exit code = %d, want the program's 23", code)
	}
	output := filepath.Join(dir, "hello.py")
	if data, err := os.ReadFile(output); err != nil || string(data) != source {
		t.Errorf("generated file = %q, %v; want %q", data, err, source)
	}
	if data, err := os.ReadFile(input); err != nil || string(data) != source {
		t.Errorf("input file = %q, %v; want it untouched", data, err)
	}
	wantArgs := []string{output, "world"}
	if len(gotPlan.steps) != 1 || !reflect.DeepEqual(gotPlan.steps[0].args, wantArgs) {
		t.Errorf("execution plan = %#v, want args %#v", gotPlan.steps, wantArgs)
	}
	if info, err := os.Stat(output); err != nil {
		t.Errorf("stat generated file: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Errorf("generated mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestRunCompiledTranslatesPythonSlotsWithoutResolvingThem(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "hello.py.psl")
	const source = "print(:: greeting as a quoted string ::)\nprint(:: farewell as a quoted string ::)\n"
	if err := os.WriteFile(input, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	withFakeExecution(t, func(context.Context, executionPlan, io.Reader, io.Writer, io.Writer) (int, error) {
		return 0, nil
	})
	var progress strings.Builder
	code, err := runCompiled(context.Background(), compiler.Options{
		Path: input,
	}, nil, strings.NewReader(""), io.Discard, &progress)
	if err != nil || code != 0 {
		t.Fatalf("runCompiled() = %d, %v", code, err)
	}
	generated, err := os.ReadFile(filepath.Join(dir, "hello.py"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(generated); strings.Count(got, `check_output`) != 2 ||
		!strings.Contains(got, `:: greeting as a quoted string ::`) ||
		!strings.Contains(got, `:: farewell as a quoted string ::`) {
		t.Errorf("generated source = %q, want two runtime PSL calls", got)
	}
	if original, err := os.ReadFile(input); err != nil || string(original) != source {
		t.Errorf("input = %q, %v; want it untouched", original, err)
	}
	if !strings.Contains(progress.String(), "translated 2 runtime slot(s)") || !strings.Contains(progress.String(), "psl: wrote ") {
		t.Errorf("progress = %q, want the translation count and output path", progress.String())
	}
}

func TestTranslatePythonRuntimeEvaluatesAnFStringAtEachCallSite(t *testing.T) {
	const source = "for i in range(2):\n    print(f\":: give me value number {i} ::\")\n"
	translated, count, err := translatePythonRuntime(source, pythonLanguage(t), "/usr/local/bin/psl", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("translated slots = %d, want 1", count)
	}
	want := `print(__import__("subprocess").check_output(["/usr/local/bin/psl", "resolve", f":: give me value number {i} ::"], text=True))`
	if !strings.Contains(translated, want) {
		t.Errorf("translated source = %q, want runtime f-string call %q", translated, want)
	}
}

func TestTranslatePythonRuntimeRejectsPartialStringsAndComments(t *testing.T) {
	for name, source := range map[string]string{
		"partial string": `message = "news: :: today in Tokyo ::"`,
		"comment":        `# :: write the loop ::`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := translatePythonRuntime(source, pythonLanguage(t), "psl", compiler.Options{Path: "app.py.psl"}); err == nil {
				t.Fatal("translation succeeded, want a runtime-slot placement error")
			}
		})
	}
}

func pythonLanguage(t *testing.T) *lang.Language {
	t.Helper()
	language, _, err := lang.Of("app.py.psl")
	if err != nil {
		t.Fatal(err)
	}
	return language
}
