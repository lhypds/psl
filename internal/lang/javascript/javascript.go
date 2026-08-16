// Package javascript holds JavaScript's rules.
//
// JavaScript has no `::` operator — the bind proposal that wanted one never
// landed — so every `::` in a .js file is a slot or it is text. The trouble is
// how many places text can hide in:
//
//	const tpl = `grid-area: ${name}::marker`;   template literal
//	const re  = /^[a-f0-9:]+::1$/;              regular expression
//	el.querySelector("li::before");             ordinary string
//
// The regular expression is the one that needs real work, because a slash is
// division just as often as it is a literal. The two are told apart the way
// every JavaScript tokeniser tells them apart: by what comes before. After a
// value — an identifier, a number, a closing bracket, a string — a slash
// divides; anywhere else it opens a pattern. The keywords that end in a letter
// but are not values (return, typeof, case) have to be listed out, which is
// the usual price of that rule.
package javascript

import (
	"unicode/utf8"

	"psl/internal/lang"
)

// Language is JavaScript: the C comment pair, and every literal a slash, a
// quote or a backtick can open.
var Language = lang.Register(&lang.Language{
	Name:          "JavaScript",
	Exts:          []string{"js", "jsx", "mjs", "cjs"},
	Comment:       lang.CComments,
	String:        Literal,
	ExecutionPlan: ExecutionPlan,
})

// Literal matches a string, a template literal, or a regular expression. It is
// exported because TypeScript is JavaScript here, and says so by reaching for
// this rather than by copying it.
func Literal(src string, i int) (int, bool) {
	switch src[i] {
	case '"', '\'':
		q := src[i : i+1]
		return lang.Quoted(src, i, q, q, '\\', true)
	case '`':
		// A template literal runs over newlines, and its ${...} holes are left
		// inside it: a slot written in one still pairs up, and a `::` in one
		// still cannot reach out.
		return lang.Quoted(src, i, "`", "`", '\\', false)
	case '/':
		return regex(src, i)
	}
	return 0, false
}

// regex matches a regular expression literal. Comments are matched before this
// is reached, so a slash here is either division or a pattern.
func regex(src string, i int) (int, bool) {
	if !regexPosition(src, i) {
		return 0, false
	}
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case '\n':
			// A pattern cannot span lines, so an unpaired slash was division
			// after all — or prose, in an instruction that mentions and/or.
			return 0, false
		case '[':
			// A character class may hold an unescaped slash.
			for j++; j < len(src) && src[j] != ']' && src[j] != '\n'; j++ {
				if src[j] == '\\' {
					j++
				}
			}
		case '/':
			return j + 1, true
		}
	}
	return 0, false
}

// regexPosition reports whether a slash at i can open a pattern rather than
// divide what precedes it.
func regexPosition(src string, i int) bool {
	j := i
	for j > 0 && (src[j-1] == ' ' || src[j-1] == '\t') {
		j--
	}
	if j == 0 {
		return true
	}
	switch src[j-1] {
	case ')', ']', '}', '"', '\'', '`':
		return false
	}
	prev, _ := utf8.DecodeLastRuneInString(src[:j])
	if !lang.IsIdentRune(prev) {
		return true
	}
	// An identifier or a number divides; a keyword that happens to end in a
	// letter does not.
	start := j
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(src[:start])
		if !lang.IsIdentRune(r) {
			break
		}
		start -= size
	}
	return regexKeywords[src[start:j]]
}

// regexKeywords are the words a regular expression may follow. Everything else
// ending in an identifier is a value, and a slash after a value divides it.
var regexKeywords = map[string]bool{
	"await": true, "case": true, "delete": true, "do": true, "else": true,
	"in": true, "instanceof": true, "new": true, "of": true, "return": true,
	"throw": true, "typeof": true, "void": true, "yield": true,
}
