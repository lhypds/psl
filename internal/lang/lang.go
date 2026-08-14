// Package lang holds, one folder per language, the rules that keep PSL's `::`
// delimiters from colliding with a language's own syntax.
//
// A PSL file is named after the language it is written in, and the extension
// before `.psl` is what selects the rules:
//
//	fib.go.psl      Go
//	bot.py.psl      Python
//	Program.cs.psl  C#
//
// An extension no language claims falls back to [Generic], whose rules are the
// ones every language shares — so an unsupported language still compiles, just
// without anything written for it in particular.
//
// The files here are the machinery: the [Language] a folder's rules are written
// as, the [Syntax] one source file is read into, and the matchers those rules
// are assembled from. Each language lives in a folder beside them —
// [psl/internal/lang/python], [psl/internal/lang/rust] — holding that
// language's rules and the tests that hold them to it. A folder registers
// itself with [Register] when it is imported, and psl links the set of them in
// one place, [psl/internal/compiler].
//
// Adding a language means adding one folder: a [Language] value registered with
// its extensions, the comment and literal forms the scanner has to step over,
// and whatever guards that language's own `::` needs.
package lang

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// naming is the one shape a PSL file's name may have. It is not a convention
// psl could guess its way around: the language decides which `::` are slots,
// so a file that does not say which language it is written in cannot be
// parsed at all.
const naming = "a PSL file is named <name>.<language>.psl, " +
	"so psl knows whose syntax to keep out of the way of — main.py.psl, Program.cs.psl, fib.go.psl"

// Of returns the language a PSL file is written in, together with the
// extension it was read from. An extension no language claims is not an error:
// the file compiles under Generic, and the caller is left to say so.
func Of(path string) (*Language, string, error) {
	base := filepath.Base(path)
	if !strings.EqualFold(filepath.Ext(base), ".psl") {
		return nil, "", fmt.Errorf("%s: not a .psl file: %s", path, naming)
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(base[:len(base)-len(".psl")]), "."))
	if ext == "" {
		return nil, "", fmt.Errorf("%s: no language in the name: %s", path, naming)
	}
	if l, ok := registry[ext]; ok {
		return l, ext, nil
	}
	return Generic, ext, nil
}

// Supported lists the languages whose folders are linked in, by name.
func Supported() []*Language {
	seen := make(map[*Language]bool, len(registry))
	out := make([]*Language, 0, len(registry))
	for _, l := range registry {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// Extensions lists every extension a language claims, sorted.
func Extensions() []string {
	out := make([]string, 0, len(registry))
	for ext := range registry {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// registry maps an extension to the language that claims it.
var registry = map[string]*Language{}

// Register adds a language to the table and hands it back, so a language
// folder is one `var Language = lang.Register(&lang.Language{...})` and nothing
// else. A folder that is never imported is a language psl does not have: the
// imports that fix the set live in [psl/internal/compiler].
//
// Two folders claiming one extension is a mistake that must not be settled by
// whichever package variable happened to be initialised first, so it panics.
// Every language folder is loaded by any `go test ./...`, which is early enough
// that this can never reach a user.
func Register(l *Language) *Language {
	if len(l.Exts) == 0 {
		panic("lang: " + l.Name + " claims no extension")
	}
	for _, ext := range l.Exts {
		if other, dup := registry[ext]; dup {
			panic(fmt.Sprintf("lang: %s and %s both claim .%s", other.Name, l.Name, ext))
		}
		registry[ext] = l
	}
	return l
}
