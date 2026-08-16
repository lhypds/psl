// Package python holds Python's rules.
//
// Python is the one language here whose own `::` is not scope resolution but a
// slice with its middle left out:
//
//	last  = xs[-1]
//	rev   = xs[::-1]
//	evens = xs[::2]
//	whole = xs[::]
//
// Those all sit inside a subscript, and none of them puts whitespace where a
// slot puts it — a slice writes its step straight after the colons, while an
// instruction starts with a word and ends after one. So inside brackets a `::`
// opens a slot only when a space follows it and closes one only when a space
// comes before it, which leaves xs[:: the index of the largest ::] working and
// xs[::-1] alone.
//
// The cost is the slice nobody writes: xs[a :: b], spaced out on both sides,
// would read as a delimiter. Written as Python normally is — xs[a::b] — the
// identifiers on either side settle it under the shared rule.
package python

import (
	"strings"

	"psl/internal/lang"
)

// Language is Python: the # comment, every prefix and fence a string can carry,
// and the two guards a slice needs.
var Language = lang.Register(&lang.Language{
	Name:     "Python",
	Exts:     []string{"py", "pyi", "pyw"},
	Comment:  comment,
	String:   stringLiteral,
	Brackets: true,
	Opens:    opens,
	Closes:   closes,
	TranslateRuntime: TranslateRuntime,
	ExecutionPlan:    ExecutionPlan,
})

func comment(src string, i int) (int, bool) {
	return lang.LineComment(src, i, "#")
}

// stringLiteral matches a string literal, triple-quoted or not, with any of the
// prefixes Python allows in front of it: r, b, f, u and their pairs (rb, fr).
//
// The escape stays live even under r: a raw string still cannot end on a
// backslash, so r"a\"b" runs past that quote exactly as a plain string does.
func stringLiteral(src string, i int) (int, bool) {
	j := i
	for n := 0; n < 2 && j < len(src) && isPrefix(src[j]); n++ {
		j++
	}
	if j >= len(src) || (src[j] != '"' && src[j] != '\'') {
		return 0, false
	}
	// A prefix only counts at the start of a token, so the b of a name like
	// verb does not turn the string beside it into a bytes literal.
	if j > i && !lang.StartsToken(src, i) {
		return 0, false
	}
	for _, fence := range []string{`"""`, `'''`} {
		if strings.HasPrefix(src[j:], fence) {
			return lang.Quoted(src, j, fence, fence, '\\', false)
		}
	}
	q := src[j : j+1]
	return lang.Quoted(src, j, q, q, '\\', true)
}

func isPrefix(c byte) bool {
	switch c {
	case 'r', 'R', 'b', 'B', 'f', 'F', 'u', 'U':
		return true
	}
	return false
}

// opens keeps a slice's colons from opening a slot.
func opens(sx *lang.Syntax, i int) bool {
	return !sx.InBrackets(i) || lang.SpaceAfter(sx.Source(), i)
}

// closes keeps them from closing one, which is what stops an unfinished
// instruction earlier in the file from swallowing everything down to the next
// xs[::-1].
func closes(sx *lang.Syntax, i int) bool {
	return !sx.InBrackets(i) || lang.SpaceBefore(sx.Source(), i)
}
