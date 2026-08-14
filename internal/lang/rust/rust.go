// Package rust holds Rust's rules.
//
// Rust writes `::` more often than any other language here, and not always
// between two identifiers:
//
//	use std::io::Write;                  identifiers, the shared rule has it
//	let v = Vec::<i32>::new();           turbofish: a `<` on the right
//	let d = <T as Default>::default();   qualified path: a `>` on the left
//
// The last two are what this folder adds. A `::` followed by `<` is a turbofish
// and can never close a slot; a `::` preceded by `>` finishes a qualified path
// and can never open one.
//
// Rust's literals need a folder too. A lifetime looks like the start of a
// character literal (&'a str), and a raw string can hold quotes by fencing
// itself with hashes (r#"say "hi""#) — read naively, either one puts a region
// boundary in the wrong place. Lifetimes fall out of lang.CharLiteral only
// matching a quote with a partner one character later; the hashes are handled
// here.
//
// Plain "..." is taken to end at its line. Rust does let one run over
// newlines, but an unterminated quote in an instruction is far likelier than a
// multi-line literal, and only the instruction can be lost.
package rust

import (
	"strings"

	"psl/internal/lang"
)

// Language is Rust: nested block comments, raw strings fenced with hashes, and
// the two guards a path needs.
var Language = lang.Register(&lang.Language{
	Name:    "Rust",
	Exts:    []string{"rs"},
	Comment: comment,
	String:  stringLiteral,
	Opens:   opens,
	Closes:  closes,
})

// comment is the C pair, except that Rust's block comments nest.
func comment(src string, i int) (int, bool) {
	if end, ok := lang.LineComment(src, i, "//"); ok {
		return end, true
	}
	return lang.BlockComment(src, i, "/*", "*/", true)
}

// stringLiteral matches "...", r#"..."#, b"...", br#"..."# and 'c'.
func stringLiteral(src string, i int) (int, bool) {
	if open, hashes, ok := rawOpen(src, i); ok {
		fence := `"` + strings.Repeat("#", hashes)
		k := strings.Index(src[i+open:], fence)
		if k < 0 {
			return 0, false
		}
		return i + open + k + len(fence), true
	}
	j := i
	if src[j] == 'b' {
		j++
	}
	if j >= len(src) || (j > i && !lang.StartsToken(src, i)) {
		return 0, false
	}
	switch src[j] {
	case '"':
		return lang.Quoted(src, j, `"`, `"`, '\\', true)
	case '\'':
		return lang.CharLiteral(src, j)
	}
	return 0, false
}

// rawOpen matches the opening of a raw string — r", r#", br##" — and returns
// its length together with how many hashes it fenced itself with.
func rawOpen(src string, i int) (open, hashes int, ok bool) {
	j := i
	if src[j] == 'b' {
		j++
	}
	if j >= len(src) || src[j] != 'r' || !lang.StartsToken(src, i) {
		return 0, 0, false
	}
	for j++; j < len(src) && src[j] == '#'; j++ {
		hashes++
	}
	if j >= len(src) || src[j] != '"' {
		return 0, 0, false
	}
	return j + 1 - i, hashes, true
}

// opens rejects the `::` that finishes a qualified path, <T as Trait>::f.
func opens(sx *lang.Syntax, i int) bool {
	return i == 0 || sx.Source()[i-1] != '>'
}

// closes rejects the turbofish, Vec::<i32>.
func closes(sx *lang.Syntax, i int) bool {
	src := sx.Source()
	return i+2 >= len(src) || src[i+2] != '<'
}
