package editor

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLookPath(t *testing.T) {
	t.Run("VISUAL wins", func(t *testing.T) {
		t.Setenv(EnvVisual, "true")
		t.Setenv(EnvEditor, "false")
		got, err := LookPath()
		if err != nil {
			t.Fatalf("LookPath() error: %v", err)
		}
		if len(got) != 1 || got[0] != "true" {
			t.Errorf("LookPath() = %q, want $%s to be preferred", got, EnvVisual)
		}
	})

	t.Run("EDITOR when VISUAL is unset", func(t *testing.T) {
		t.Setenv(EnvVisual, "")
		t.Setenv(EnvEditor, "true")
		got, err := LookPath()
		if err != nil {
			t.Fatalf("LookPath() error: %v", err)
		}
		if len(got) != 1 || got[0] != "true" {
			t.Errorf("LookPath() = %q, want the $%s value", got, EnvEditor)
		}
	})

	t.Run("arguments are kept", func(t *testing.T) {
		t.Setenv(EnvVisual, "")
		t.Setenv(EnvEditor, "true --wait")
		got, err := LookPath()
		if err != nil {
			t.Fatalf("LookPath() error: %v", err)
		}
		if strings.Join(got, " ") != "true --wait" {
			t.Errorf("LookPath() = %q, want the arguments the variable carried", got)
		}
	})

	t.Run("an editor that is not installed is an error", func(t *testing.T) {
		t.Setenv(EnvVisual, "")
		t.Setenv(EnvEditor, "psl-no-such-editor")
		_, err := LookPath()
		if err == nil {
			t.Fatal("LookPath() succeeded, want an error naming the missing editor")
		}
		for _, want := range []string{EnvEditor, "psl-no-such-editor"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("LookPath() error = %v, want it to mention %q", err, want)
			}
		}
	})

	t.Run("fallback when neither is set", func(t *testing.T) {
		t.Setenv(EnvVisual, "")
		t.Setenv(EnvEditor, "")
		got, err := LookPath()
		if err != nil {
			// A machine with none of the fallbacks installed is what the error
			// is for; the message is then the whole of the behaviour.
			if !strings.Contains(err.Error(), EnvEditor) {
				t.Errorf("LookPath() error = %v, want it to say which variable to set", err)
			}
			return
		}
		if len(got) == 0 {
			t.Fatal("LookPath() returned an empty command")
		}
		if !slices.Contains(fallbacks(), got[0]) {
			t.Errorf("LookPath() = %q, want one of %q", got, fallbacks())
		}
	})
}

func TestOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the editor stub is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".pslrc")
	if err := os.WriteFile(path, []byte("default_model=m\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A stub editor that records the file it was handed, which is the one thing
	// Open is responsible for getting right.
	stub := filepath.Join(dir, "stub-editor")
	record := filepath.Join(dir, "opened")
	script := "#!/bin/sh\necho \"$@\" > " + record + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvVisual, "")
	t.Setenv(EnvEditor, stub+" --wait")
	if err := Open(path); err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "--wait "+path {
		t.Errorf("editor ran with %q, want its own arguments then %q", got, path)
	}

	t.Run("a failing editor is an error", func(t *testing.T) {
		t.Setenv(EnvEditor, "false")
		if err := Open(path); err == nil {
			t.Fatal("Open() succeeded, want the editor's failure reported")
		}
	})
}
