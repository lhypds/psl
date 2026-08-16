// Package golang holds Go's rules.
//
// Go has no `::` at all: not scope resolution, not a slice step, nothing. Every
// `::` in a Go file is either a slot or text inside a literal, which makes this
// the shortest language folder here — the shared rule and Go's three kinds of
// literal are the whole of it.
//
// The raw string is the one that matters. It runs over newlines and has no
// escapes, so a struct tag or an embedded query can hold anything:
//
//	var tmpl = `dial ::1 first`
//
// Recognising it keeps that `::` from pairing with the next one in the file.
//
// The folder is golang and not go because go is a keyword: a package cannot be
// named one.
package golang

import "psl/internal/lang"

// Language is Go: the C comment pair, and Go's three kinds of literal.
var Language = lang.Register(&lang.Language{
	Name:          "Go",
	Exts:          []string{"go"},
	Comment:       lang.CComments,
	String:        stringLiteral,
	ExecutionPlan: ExecutionPlan,
})

// stringLiteral matches an interpreted string, a raw string, or a rune literal.
func stringLiteral(src string, i int) (int, bool) {
	switch src[i] {
	case '"':
		return lang.Quoted(src, i, `"`, `"`, '\\', true)
	case '`':
		return lang.Quoted(src, i, "`", "`", 0, false)
	case '\'':
		return lang.CharLiteral(src, i)
	}
	return 0, false
}
