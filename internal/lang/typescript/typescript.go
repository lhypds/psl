// Package typescript holds TypeScript's rules.
//
// TypeScript is JavaScript plus a type language, and the type language has no
// `::` in it — a type is written with `:`, generics with angle brackets, and
// namespaces with a dot. So every `::` in a .ts file is a slot, or it is hiding
// in exactly the places it hides in JavaScript, and the rules are JavaScript's
// rules: the same comments, the same strings and template literals, and the
// same slash that has to be told apart from division.
//
// This folder exists to say that on purpose. A language that shares another's
// hazards is worth registering explicitly, so that adding a TypeScript-only
// rule later means editing typescript and not javascript.
package typescript

import (
	"psl/internal/lang"
	"psl/internal/lang/javascript"
)

// Language is TypeScript: JavaScript's rules, claimed under TypeScript's
// extensions and TypeScript's name.
var Language = lang.Register(&lang.Language{
	Name:    "TypeScript",
	Exts:    []string{"ts", "tsx", "mts", "cts"},
	Comment: lang.CComments,
	String:  javascript.Literal,
})
