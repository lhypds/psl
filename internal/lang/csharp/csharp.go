// Package csharp holds C#'s rules.
//
// C# writes `::` only as the namespace alias qualifier — global::System,
// MyAlias::Widget — which is always an identifier on both sides, so the shared
// rule covers every `::` the language itself can produce.
//
// What C# needs a folder for is how many ways it spells a string. A verbatim
// string runs over newlines and escapes its quote by doubling it, a raw string
// is fenced by three or more quotes, and both can carry a $ for interpolation:
//
//	var path = @"C:\logs\""old""";
//	var json = """{"addr": "::1"}""";
//
// Reading those as ordinary quoted strings would put the region boundaries in
// the wrong places, and a `::` sitting in one would leak out of it.
package csharp

import (
	"strings"

	"psl/internal/lang"
)

// Language is C#: the C comment pair, and every way C# spells a string.
var Language = lang.Register(&lang.Language{
	Name:    "C#",
	Exts:    []string{"cs", "csx"},
	Comment: lang.CComments,
	String:  stringLiteral,
	ExecutionPlan: ExecutionPlan,
})

// stringLiteral matches "...", @"...", $"...", $@"...", """...""" and 'c'.
func stringLiteral(src string, i int) (int, bool) {
	// $ and @ are the only prefixes, in either order and any number of $ for a
	// raw interpolated string ($$""" ... """).
	j, verbatim := i, false
	for j < len(src) && (src[j] == '$' || src[j] == '@') {
		verbatim = verbatim || src[j] == '@'
		j++
	}
	// @name is a verbatim identifier, not a string; it falls out here because
	// no quote follows. A prefix still only counts at the start of a token.
	if j >= len(src) || (j > i && !lang.StartsToken(src, i)) {
		return 0, false
	}
	switch {
	case strings.HasPrefix(src[j:], `"""`):
		return rawString(src, j)
	case src[j] == '"' && verbatim:
		return verbatimString(src, j)
	case src[j] == '"':
		return lang.Quoted(src, j, `"`, `"`, '\\', true)
	case src[j] == '\'' && j == i:
		return lang.CharLiteral(src, i)
	}
	return 0, false
}

// verbatimString matches the body of an @"..." string from its opening quote.
// A doubled quote is an escaped one; a lone quote ends the string. Backslashes
// mean nothing, and newlines are allowed.
func verbatimString(src string, i int) (int, bool) {
	for j := i + 1; j < len(src); j++ {
		if src[j] != '"' {
			continue
		}
		if j+1 < len(src) && src[j+1] == '"' {
			j++
			continue
		}
		return j + 1, true
	}
	return 0, false
}

// rawString matches a """ ... """ literal. The opening run of quotes sets the
// length of the closing one, which is how a raw string holds quotes of its own.
func rawString(src string, i int) (int, bool) {
	n := 0
	for i+n < len(src) && src[i+n] == '"' {
		n++
	}
	if n < 3 {
		return 0, false
	}
	fence := strings.Repeat(`"`, n)
	if k := strings.Index(src[i+n:], fence); k >= 0 {
		return i + n + k + n, true
	}
	return 0, false
}
