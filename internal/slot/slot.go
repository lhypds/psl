// Package slot locates and rewrites PSL AI slots inside a source file.
//
// A slot is text wrapped in `::` delimiters:
//
//	:: write a fibonacci function ::
//	:: gpt-5.6> write a fibonacci function ::
//
// Because PSL files are written in other languages, the delimiters are only
// recognised when they cannot be part of that language's own syntax. Scope
// resolution — C++'s std::cout, Rust's Foo::Bar, PHP's self::method — always
// glues `::` to an identifier, so:
//
//   - the opening `::` must not be glued to an identifier on its left;
//   - the closing `::` must not be glued to an identifier on its right.
//
// Python's extended slices glue `::` to brackets and step values instead —
// xs[::2], xs[::-1], xs[::step], a[::2, ::-1] — so additionally:
//
//   - a `::` glued to `[` on its left opens a slot only when followed by
//     whitespace, which is how `[:: three primes ::]` stays a slot while
//     xs[::step] stays a slice;
//   - a `::` glued to a digit, `-`, `,` or `]` on its right is a slice's
//     step, never a delimiter.
//
// Whitespace inside the delimiters is optional: `::do it::` and `:: do it ::`
// are the same slot.
package slot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Marker is substituted for a slot when the file is shown to the model, so the
// model can see exactly where its output will land.
const Marker = "⟦PSL_SLOT⟧"

// Slot is a single AI instruction found in a source file.
type Slot struct {
	Start       int    // byte offset of the opening "::"
	End         int    // byte offset just past the closing "::"
	Model       string // model name written as "model>", empty when unspecified
	Instruction string // instruction text, trimmed
	Indent      string // leading whitespace of the line, when the slot starts the line
}

// Find returns the first slot in src.
func Find(src string) (Slot, bool) {
	for i := 0; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
			continue
		}
		if !opensSlot(src, i) {
			continue
		}
		end, ok := findClose(src, i+2)
		if !ok {
			continue
		}
		body := src[i+2 : end]
		model, instruction := splitModel(body)
		return Slot{
			Start:       i,
			End:         end + 2,
			Model:       model,
			Instruction: strings.TrimSpace(instruction),
			Indent:      indentOf(src, i),
		}, true
	}
	return Slot{}, false
}

// Count reports how many slots src contains.
func Count(src string) int {
	n := 0
	for {
		s, ok := Find(src)
		if !ok {
			return n
		}
		n++
		src = src[s.End:]
	}
}

// Replace splices replacement into src in place of s, re-indenting continuation
// lines so that generated multi-line output lines up with the slot's column.
func Replace(src string, s Slot, replacement string) string {
	if s.Indent != "" {
		replacement = indentContinuation(replacement, s.Indent)
	}
	return src[:s.Start] + replacement + src[s.End:]
}

// Mask replaces s with Marker, producing the file as the model should see it.
func Mask(src string, s Slot) string {
	return src[:s.Start] + Marker + src[s.End:]
}

// opensSlot reports whether the "::" at i can start a slot. It cannot when an
// identifier runs into it from the left, which is what keeps the `::` of
// std::cout out of the way, and it cannot when it is glued into a slice the
// way Python writes them: xs[::2], xs[::-1], xs[::step], a[::2, ::-1].
func opensSlot(src string, i int) bool {
	prev, psize := utf8.DecodeLastRuneInString(src[:i])
	if psize != 0 && isIdentRune(prev) {
		return false
	}
	next, nsize := utf8.DecodeRuneInString(src[i+2:])
	if nsize == 0 {
		return true // nothing follows, so no closer will be found anyway
	}
	// Glued to the subscript bracket, "::" is a slice unless whitespace
	// separates it from what follows: xs[::step] is a slice, while
	// [:: three primes ::] is still a slot.
	if prev == '[' && !unicode.IsSpace(next) {
		return false
	}
	// A step glued to the right is a slice even when the "::" is not glued
	// to the bracket, as in a[::2, ::-1]. No instruction starts with these.
	if unicode.IsDigit(next) || next == '-' || next == ',' || next == ']' {
		return false
	}
	return true
}

// findClose returns the offset of the "::" that closes a slot opened at from.
// A "::" that runs straight into an identifier is scope resolution written
// inside the instruction, not the end of it; one glued to `[` on its left or
// to `-` on its right is a slice written inside the instruction, so
// `:: reverse xs with xs[::-1] ::` keeps the slice.
func findClose(src string, from int) (int, bool) {
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
			continue
		}
		next, size := utf8.DecodeRuneInString(src[i+2:])
		if size != 0 && (isIdentRune(next) || next == '-') {
			continue
		}
		if src[i-1] == '[' {
			continue
		}
		return i, true
	}
	return 0, false
}

// splitModel peels a leading "model>" off a slot body.
func splitModel(body string) (model, instruction string) {
	rest := strings.TrimLeftFunc(body, unicode.IsSpace)
	gt := strings.IndexByte(rest, '>')
	if gt <= 0 {
		return "", body
	}
	name := rest[:gt]
	if !isModelName(name) {
		return "", body
	}
	after := rest[gt+1:]
	if after != "" {
		next, _ := utf8.DecodeRuneInString(after)
		if !unicode.IsSpace(next) {
			return "", body
		}
	}
	return name, after
}

func isModelName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("._+-:/", r):
		default:
			return false
		}
	}
	return true
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// indentOf returns the line's leading whitespace when only whitespace precedes
// the slot on that line, and "" otherwise.
func indentOf(src string, start int) string {
	lineStart := strings.LastIndexByte(src[:start], '\n') + 1
	prefix := src[lineStart:start]
	if strings.TrimLeft(prefix, " \t") != "" {
		return ""
	}
	return prefix
}

func indentContinuation(text, indent string) string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
