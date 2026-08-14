// Package macro holds Macro PSL's rules — the language Pob records and
// replays, written in files named main.macro.psl.
//
// Macro PSL has no `::` of its own. It was written knowing psl was going to
// read it: a statement is a call, a block or a slot, and nowhere in the grammar
// does a pair of colons mean anything but a slot. So there is nothing here to
// keep out of the way of, and no Opens or Closes below.
//
// What the folder is for is the string. A macro types text and presses keys,
// and the text is data — an address, a URL, a snippet with colons in it:
//
//	typeText("ping ::1 first")
//
// Left alone, that `::` would go looking for a partner and find the next real
// slot further down the file. Recognising the string keeps the pair inside it,
// while a slot written inside one — typeText(":: a short reply ::") — still
// resolves, because both of its delimiters are in the same string.
//
// A string runs to the end of its line and no further. Macro PSL closes an
// unclosed string at the closing parenthesis and writes the quote back in, so
// typeText("::what to say::) holds a slot that psl has to see; a literal that
// runs to the end of the file would swallow it. That is the same rule psl uses
// everywhere, for the same reason.
//
// Comments are C's, `//` and `/* … */`, and they hold slots as often as code
// does. Pob writes the `::` markers out of a comment before handing psl the
// file, so what arrives here is already a comment with nothing to fill in it.
package macro

import "psl/internal/lang"

// Language is Macro PSL: the C comment pair, and the one string it writes.
var Language = lang.Register(&lang.Language{
	Name:    "Macro PSL",
	Exts:    []string{"macro"},
	Comment: lang.CComments,
	String:  stringLiteral,
})

// stringLiteral matches the double-quoted string, the only literal the language
// has. A number is bare, and so is a time — 250ms, 10h5m — so neither can hold
// a quote or a colon to be confused about.
func stringLiteral(src string, i int) (int, bool) {
	if src[i] != '"' {
		return 0, false
	}
	return lang.Quoted(src, i, `"`, `"`, '\\', true)
}
