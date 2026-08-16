package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"psl/internal/compiler"
	"psl/internal/executor"
)

func withFakeExecution(t *testing.T, run func(context.Context, executor.Plan, io.Reader, io.Writer, io.Writer) (int, error)) {
	t.Helper()
	originalLookPath, originalExecute := lookPath, execute
	lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	execute = run
	t.Cleanup(func() {
		lookPath = originalLookPath
		execute = originalExecute
	})
}

func TestRunCompiledWritesTheLanguageFileAndRunsIt(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "hello.py.psl")
	const source = "print('hello')\n"
	if err := os.WriteFile(input, []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}

	var gotPlan executor.Plan
	withFakeExecution(t, func(_ context.Context, plan executor.Plan, _ io.Reader, _, _ io.Writer) (int, error) {
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
	if len(gotPlan.Steps) != 1 || !reflect.DeepEqual(gotPlan.Steps[0].Args, wantArgs) {
		t.Errorf("execution plan = %#v, want args %#v", gotPlan.Steps, wantArgs)
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

	withFakeExecution(t, func(context.Context, executor.Plan, io.Reader, io.Writer, io.Writer) (int, error) {
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
