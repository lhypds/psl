package lang

import (
	"unicode"
	"unicode/utf8"
)

// Generic is what psl falls back to when no language folder claims a file's
// extension. It knows no comments and no literals — only the one rule every
// language shares, below — so an unsupported language still compiles, with
// nothing worked around that was not written for it.
//
// Generic has no folder of its own because it is not a language: it is the
// floor every language folder stands on, and what a nil language is read as.
// It claims no extension, and is reached only by Of failing to find a better
// answer.
var Generic = &Language{Name: "generic"}

// The shared rule. Scope resolution — C++'s std::cout, Rust's Foo::Bar, PHP's
// self::method, C#'s global::System — always glues `::` to an identifier, so:
//
//   - an opening `::` must not be glued to an identifier on its left;
//   - a closing `::` must not be glued to an identifier on its right.
//
// That is what lets an instruction talk about std::cout without ending at it,
// and it holds in languages that have no scope resolution at all: a `::` run
// into a word is prose or syntax either way, never a delimiter.
//
// The price of whitespace being optional is that a `::` followed by a space
// does read as a closing delimiter, so ":: fix the std:: usage ::" ends at
// "std::". Written the ordinary way, "std::cout", it stays in the instruction.

// opensAnywhere reports whether the "::" at i can open a slot in any language.
func opensAnywhere(src string, i int) bool {
	prev, size := utf8.DecodeLastRuneInString(src[:i])
	return size == 0 || !IsIdentRune(prev)
}

// closesAnywhere reports whether the "::" at i can close a slot in any
// language.
func closesAnywhere(src string, i int) bool {
	next, size := utf8.DecodeRuneInString(src[i+2:])
	return size == 0 || !IsIdentRune(next)
}

// IsIdentRune reports whether r can sit inside an identifier, which is the
// whole of what the shared rule needs to know about a language.
func IsIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
