package lang

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"psl/internal/executor"
)

// RuntimeOptions is the language-independent context a runtime source
// translator may capture in generated calls.
type RuntimeOptions struct {
	Path           string
	Executable     string
	Prompt         string
	ImageMediaType string
	ImageBase64    string
}

// Language is one language's answer to two questions: which parts of a source
// file psl must not read a delimiter out of, and which `::` in it are the
// language's own syntax rather than a slot.
type Language struct {
	// Name is the language as a person writes it — "Python", "C#". It is what
	// psl reports, and what the model is told the file is written in.
	Name string

	// Exts are the extensions that select this language, without the dot.
	Exts []string

	// Comment returns the offset just past the comment starting at i, and
	// whether one starts there at all. Comments are not opaque — a slot sits
	// in a comment as often as in code — but they are recognised so that a
	// quote inside one cannot open a string literal.
	Comment func(src string, i int) (int, bool)

	// String returns the offset just past the string or character literal
	// starting at i. A literal that is never closed is not a literal: that is
	// what keeps the apostrophe in ":: don't crash ::" from swallowing the
	// rest of the instruction.
	String func(src string, i int) (int, bool)

	// Brackets asks the scanner to track `[` ... `]` nesting, for the
	// languages whose subscript syntax can hold a `::` (Python's slices).
	Brackets bool

	// Opens and Closes veto a `::` that the shared rules in generic.go would
	// have accepted. They run last, so a language folder only ever has to
	// state what is peculiar to it.
	Opens  func(sx *Syntax, i int) bool
	Closes func(sx *Syntax, i int) bool

	// TranslateRuntime converts source slots into calls made when the generated
	// program reaches them. Nil means this language keeps compile-time slots.
	TranslateRuntime func(source string, opts RuntimeOptions) (string, int, error)

	// ExecutionPlan chooses how psl run executes the generated language file.
	// Nil means the language has no conventional executor.
	ExecutionPlan func(ext, source string, programArgs []string, lookPath executor.LookPath) (executor.Plan, error)
}

// Syntax is one source file read under one language's rules: where its
// comments and literals are, and how deep in brackets each byte sits. The
// parser asks it two questions per `::` and nothing else.
type Syntax struct {
	lang    *Language
	src     string
	regions []region // comments and literals, in order, never overlapping
	bracket []bool   // inside `[` ... `]`, when the language asks for it
}

// region is one comment or one literal: a run of the file a slot may not
// straddle.
type region struct {
	start, end int
	kind       regionKind
}

type regionKind byte

const (
	commentRegion regionKind = iota
	stringRegion
)

// Analyze reads src once under l's rules. A nil language reads it under
// Generic, which is the same as not reading it at all.
func (l *Language) Analyze(src string) *Syntax {
	if l == nil {
		l = Generic
	}
	sx := &Syntax{lang: l, src: src}
	if l.Brackets {
		sx.bracket = make([]bool, len(src))
	}
	depth := 0
	for i := 0; i < len(src); {
		if r, ok := l.region(src, i); ok {
			sx.regions = append(sx.regions, r)
			if sx.bracket != nil {
				for j := i; j < r.end; j++ {
					sx.bracket[j] = depth > 0
				}
			}
			i = r.end
			continue
		}
		if sx.bracket != nil {
			switch src[i] {
			case '[':
				depth++
			case ']':
				if depth > 0 {
					depth--
				}
			}
			sx.bracket[i] = depth > 0
		}
		i++
	}
	return sx
}

// region matches the comment or literal starting at i. Comments are tried
// first: a quote inside a comment starts nothing, and neither does a comment
// marker inside a string.
func (l *Language) region(src string, i int) (region, bool) {
	if l.Comment != nil {
		if end, ok := l.Comment(src, i); ok && end > i {
			return region{start: i, end: end, kind: commentRegion}, true
		}
	}
	if l.String != nil {
		if end, ok := l.String(src, i); ok && end > i {
			return region{start: i, end: end, kind: stringRegion}, true
		}
	}
	return region{}, false
}

// Source is the file Analyze read.
func (sx *Syntax) Source() string { return sx.src }

// Language is what it was read as.
func (sx *Syntax) Language() *Language { return sx.lang }

// CanOpen reports whether the "::" at i may open a slot.
func (sx *Syntax) CanOpen(i int) bool {
	if !opensAnywhere(sx.src, i) {
		return false
	}
	return sx.lang.Opens == nil || sx.lang.Opens(sx, i)
}

// CanClose reports whether the "::" at i may close a slot opened at from.
//
// A slot never straddles a comment or a literal: a `::` inside a string pairs
// only with a `::` in the same string. That is what stops the "::" of a lone
// "::1" from reaching across the file to the next delimiter, while a slot
// written inside a string — x = ":: the greeting ::" — still resolves.
func (sx *Syntax) CanClose(from, i int) bool {
	if sx.regionAt(from) != sx.regionAt(i) {
		return false
	}
	if !closesAnywhere(sx.src, i) {
		return false
	}
	return sx.lang.Closes == nil || sx.lang.Closes(sx, i)
}

// InBrackets reports whether i sits inside `[` ... `]`. It is always false
// unless the language asked for brackets to be tracked.
func (sx *Syntax) InBrackets(i int) bool {
	return sx.bracket != nil && i < len(sx.bracket) && sx.bracket[i]
}

// StringAt reports the bounds of the string literal containing i. It is used
// by source translators that need to replace a slot together with the literal
// quotes around it. The bounds include any literal prefix and its fences.
func (sx *Syntax) StringAt(i int) (start, end int, ok bool) {
	r, ok := sx.regionHolding(i)
	if !ok || r.kind != stringRegion {
		return 0, 0, false
	}
	return r.start, r.end, true
}

// InComment reports whether i is inside a comment.
func (sx *Syntax) InComment(i int) bool {
	r, ok := sx.regionHolding(i)
	return ok && r.kind == commentRegion
}

// regionAt returns the index of the comment or literal holding i, and -1 when
// i is in code. Two offsets are in the same context when this agrees on them.
func (sx *Syntax) regionAt(i int) int {
	n := sort.Search(len(sx.regions), func(k int) bool { return sx.regions[k].end > i })
	if n < len(sx.regions) && sx.regions[n].start <= i {
		return n
	}
	return -1
}

func (sx *Syntax) regionHolding(i int) (region, bool) {
	n := sx.regionAt(i)
	if n < 0 {
		return region{}, false
	}
	return sx.regions[n], true
}

// The helpers below are the pieces a language folder is built out of. They all
// take the offset a construct might start at and return the offset just past
// it, so they compose into a Language's Comment and String.

// LineComment matches a comment that runs from one of the markers to the end
// of its line. The newline is left out of it, so the next line starts clean.
func LineComment(src string, i int, markers ...string) (int, bool) {
	for _, marker := range markers {
		if !strings.HasPrefix(src[i:], marker) {
			continue
		}
		if nl := strings.IndexByte(src[i:], '\n'); nl >= 0 {
			return i + nl, true
		}
		return len(src), true
	}
	return 0, false
}

// BlockComment matches open ... close, counting nesting when the language
// allows it (Rust). A comment that is never closed is not treated as one:
// an instruction that mentions /* would otherwise swallow the rest of itself.
func BlockComment(src string, i int, open, close string, nest bool) (int, bool) {
	if !strings.HasPrefix(src[i:], open) {
		return 0, false
	}
	depth, j := 1, i+len(open)
	for j < len(src) {
		switch {
		case strings.HasPrefix(src[j:], close):
			j += len(close)
			if depth--; depth == 0 {
				return j, true
			}
		case nest && strings.HasPrefix(src[j:], open):
			depth++
			j += len(open)
		default:
			j++
		}
	}
	return 0, false
}

// Quoted matches a literal opening with open and closing with close, esc being
// its escape byte (0 when it has none) and line saying whether it ends at the
// end of the line.
//
// A literal that is never closed is not a literal. That single decision is
// what lets an instruction be written in English: the apostrophe in
// ":: don't crash ::" finds no partner before the newline, so it stays an
// apostrophe instead of opening a string over the closing delimiter.
func Quoted(src string, i int, open, close string, esc byte, line bool) (int, bool) {
	if !strings.HasPrefix(src[i:], open) {
		return 0, false
	}
	for j := i + len(open); j < len(src); {
		switch {
		case line && src[j] == '\n':
			return 0, false
		case esc != 0 && src[j] == esc:
			j += 2
		case strings.HasPrefix(src[j:], close):
			return j + len(close), true
		default:
			j++
		}
	}
	return 0, false
}

// CharLiteral matches a C-family character literal: a quote, one character or
// one escape, and a closing quote. Nothing longer is one, which is how Rust's
// lifetimes (&'a str) and the apostrophe in an instruction stay out of the
// way — both are a quote with no partner one character later.
func CharLiteral(src string, i int) (int, bool) {
	if i >= len(src) || src[i] != '\'' {
		return 0, false
	}
	j := i + 1
	if j < len(src) && src[j] == '\\' {
		// Past the backslash and whatever it escapes — which may itself be the
		// quote, as in '\'' — and then on to the end of a longer escape. The
		// longest worth allowing is a unicode one, '\u{1F600}'.
		j += 2
		for ; j < len(src) && j < i+12 && src[j] != '\'' && src[j] != '\n'; j++ {
		}
	} else {
		_, size := utf8.DecodeRuneInString(src[j:])
		if size == 0 {
			return 0, false
		}
		j += size
	}
	if j < len(src) && src[j] == '\'' {
		return j + 1, true
	}
	return 0, false
}

// CComments is the `//` and `/* */` pair that C, C#, Go, JavaScript and
// TypeScript all write comments with.
func CComments(src string, i int) (int, bool) {
	if end, ok := LineComment(src, i, "//"); ok {
		return end, true
	}
	return BlockComment(src, i, "/*", "*/", false)
}

// StartsToken reports whether i begins a token rather than continuing an
// identifier, so that the r ending a name like ptr is not read as Python's
// raw-string prefix on the string that follows it.
func StartsToken(src string, i int) bool {
	prev, size := utf8.DecodeLastRuneInString(src[:i])
	return size == 0 || !IsIdentRune(prev)
}

// SpaceAfter reports whether the "::" at i is followed by whitespace, and
// SpaceBefore whether it is preceded by some. A slot written by hand has one
// or the other; the syntax it is mistaken for usually has neither.
func SpaceAfter(src string, i int) bool {
	r, size := utf8.DecodeRuneInString(src[i+2:])
	return size != 0 && unicode.IsSpace(r)
}

func SpaceBefore(src string, i int) bool {
	r, size := utf8.DecodeLastRuneInString(src[:i])
	return size != 0 && unicode.IsSpace(r)
}
