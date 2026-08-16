// Package slot locates and rewrites PSL AI slots inside a source file.
//
// A slot is text wrapped in `::` delimiters:
//
//	:: write a fibonacci function ::
//	:: gpt-5.6> write a fibonacci function ::
//
// Whitespace inside the delimiters is optional: `::do it::` and `:: do it ::`
// are the same slot.
//
// Because PSL files are written in other languages, a `::` is only a delimiter
// when it cannot be that language's own syntax. What counts as its own syntax
// is not decided here: every rule lives in [psl/internal/lang], one folder per
// language, and this package only asks that package about each `::` it finds.
package slot

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"psl/internal/lang"
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

// Find returns the first slot in src, read under l's rules. A nil language
// reads it under lang.Generic.
func Find(src string, l *lang.Language) (Slot, bool) {
	return find(l.Analyze(src), 0)
}

// All returns every slot in src in source order.
func All(src string, l *lang.Language) []Slot {
	sx := l.Analyze(src)
	var slots []Slot
	for from := 0; ; {
		s, ok := find(sx, from)
		if !ok {
			return slots
		}
		slots = append(slots, s)
		from = s.End
	}
}

// Count reports how many slots src holds.
func Count(src string, l *lang.Language) int {
	sx := l.Analyze(src)
	n, from := 0, 0
	for {
		s, ok := find(sx, from)
		if !ok {
			return n
		}
		n++
		from = s.End
	}
}

// find returns the first slot at or after from. The analysis is done once and
// reused, so counting a file costs one pass over it rather than one per slot.
func find(sx *lang.Syntax, from int) (Slot, bool) {
	src := sx.Source()
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' || !sx.CanOpen(i) {
			continue
		}
		end, ok := findClose(sx, i)
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

// findClose returns the offset of the "::" that closes the slot opened at open.
func findClose(sx *lang.Syntax, open int) (int, bool) {
	src := sx.Source()
	for i := open + 2; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' || !sx.CanClose(open, i) {
			continue
		}
		return i, true
	}
	return 0, false
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
