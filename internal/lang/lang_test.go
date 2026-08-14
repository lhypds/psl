// The tests here are the ones that need every language at once: which folder a
// name resolves to, and that the table they register in is whole. They import
// the language folders the way psl does, which is also what proves a folder is
// reachable at all.
package lang_test

import (
	"strings"
	"testing"

	"psl/internal/lang"
	"psl/internal/lang/c"
	"psl/internal/lang/csharp"
	"psl/internal/lang/golang"
	"psl/internal/lang/javascript"
	"psl/internal/lang/python"
	"psl/internal/lang/rust"
	"psl/internal/lang/typescript"
)

func TestOf(t *testing.T) {
	tests := []struct {
		path string
		lang *lang.Language
		ext  string
	}{
		{"fib.go.psl", golang.Language, "go"},
		{"bot.py.psl", python.Language, "py"},
		{"Program.cs.psl", csharp.Language, "cs"},
		{"main.c.psl", c.Language, "c"},
		{"lib.rs.psl", rust.Language, "rs"},
		{"app.jsx.psl", javascript.Language, "jsx"},
		{"app.tsx.psl", typescript.Language, "tsx"},
		{"/a/b/ui.ts.psl", typescript.Language, "ts"},
		// The extension is a name, not a spelling.
		{"FIB.GO.PSL", golang.Language, "go"},
		// A dotted stem keeps its last extension, not its first.
		{"my.old.bot.py.psl", python.Language, "py"},
		// No folder claims .zig, so it compiles under the shared rules alone.
		{"app.zig.psl", lang.Generic, "zig"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			l, ext, err := lang.Of(tc.path)
			if err != nil {
				t.Fatalf("Of(%q) error: %v", tc.path, err)
			}
			if l != tc.lang {
				t.Errorf("Of(%q) = %s, want %s", tc.path, l.Name, tc.lang.Name)
			}
			if ext != tc.ext {
				t.Errorf("Of(%q) ext = %q, want %q", tc.path, ext, tc.ext)
			}
		})
	}
}

// A name that does not say which language the file is written in cannot be
// compiled: the language is what decides which `::` are slots.
func TestOfRejectsNamesWithoutALanguage(t *testing.T) {
	for _, path := range []string{"fib.psl", "fib.go", "fib", ".psl", "fib..psl", "fib.psl.go"} {
		t.Run(path, func(t *testing.T) {
			_, _, err := lang.Of(path)
			if err == nil {
				t.Fatalf("Of(%q) succeeded, want the naming rule enforced", path)
			}
			if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "main.py.psl") {
				t.Errorf("error = %v, want it to name the file and show the shape", err)
			}
		})
	}
}

// Every language folder must be reachable, and no two may claim one extension —
// Register panics on a collision, so simply importing them proves it.
func TestRegistryIsComplete(t *testing.T) {
	if len(lang.Supported()) < 7 {
		t.Errorf("Supported() = %d languages, want every language folder registered", len(lang.Supported()))
	}
	for _, l := range lang.Supported() {
		if l.Name == "" {
			t.Error("a language folder registered without a name")
		}
		for _, ext := range l.Exts {
			got, _, err := lang.Of("x." + ext + ".psl")
			if err != nil || got != l {
				t.Errorf(".%s resolves to %v (%v), want %s", ext, got, err, l.Name)
			}
		}
	}
	if len(lang.Extensions()) < len(lang.Supported()) {
		t.Error("Extensions() is missing extensions")
	}
}

// Generic claims nothing, so nothing can reach it by name.
func TestGenericIsNotRegistered(t *testing.T) {
	for _, ext := range lang.Extensions() {
		if l, _, err := lang.Of("x." + ext + ".psl"); err != nil || l == lang.Generic {
			t.Errorf(".%s resolves to Generic (%v), want a language", ext, err)
		}
	}
}
