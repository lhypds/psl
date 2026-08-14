// Package c holds C's rules.
//
// C has no `::` of its own — the token does not exist in the grammar before
// C23, and the attributes C23 borrowed from C++ ([[gnu::always_inline]]) write
// it glued to identifiers on both sides, which the shared rule already keeps
// out of the way.
//
// So what this folder is really for is C's literals. A `::` inside a string is
// data, not a delimiter — an IPv6 address is the usual way one gets there:
//
//	const char *loopback = "::1";
//
// Left alone, that opening `::` would go looking for a partner and find the
// next real slot in the file. Recognising the string keeps the pair inside it.
package c

import (
	"strings"

	"psl/internal/lang"
)

// Language is C: the C comment pair, and C's literals with their encoding
// prefixes.
var Language = lang.Register(&lang.Language{
	Name:    "C",
	Exts:    []string{"c", "h"},
	Comment: lang.CComments,
	String:  stringLiteral,
})

// stringLiteral matches a string or character literal, including the encoding
// prefixes a string may carry: L"wide", u8"utf-8", u"...", U"...".
func stringLiteral(src string, i int) (int, bool) {
	if src[i] == '\'' {
		return lang.CharLiteral(src, i)
	}
	j := i + prefix(src, i)
	if j >= len(src) || src[j] != '"' {
		return 0, false
	}
	// A prefix only counts at the start of a token: the u ending a name like
	// menu is part of the name, and the string after it starts at its quote.
	if j > i && !lang.StartsToken(src, i) {
		return 0, false
	}
	return lang.Quoted(src, j, `"`, `"`, '\\', true)
}

// prefix is the length of the encoding prefix at i, if any.
func prefix(src string, i int) int {
	if strings.HasPrefix(src[i:], "u8") {
		return 2
	}
	switch src[i] {
	case 'L', 'u', 'U':
		return 1
	}
	return 0
}
