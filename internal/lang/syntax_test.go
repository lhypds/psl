package lang

import "testing"

// The tests here are about the machinery rather than about any one language, so
// they read their source under stand-ins built from the helpers themselves: the
// C comment pair and a plain double-quoted string, with and without brackets
// tracked.
var (
	clike = &Language{
		Name:    "c-like",
		Comment: CComments,
		String:  doubleQuoted,
	}
	subscripted = &Language{
		Name:     "subscripted",
		Comment:  func(src string, i int) (int, bool) { return LineComment(src, i, "#") },
		String:   doubleQuoted,
		Brackets: true,
	}
)

func doubleQuoted(src string, i int) (int, bool) {
	if src[i] != '"' {
		return 0, false
	}
	return Quoted(src, i, `"`, `"`, '\\', true)
}

// firstSlot is what the parser does, in miniature — the same scan
// internal/langtest runs the language folders through, kept here so these tests
// need nothing but this package.
func firstSlot(l *Language, src string) (string, bool) {
	sx := l.Analyze(src)
	for i := 0; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' || !sx.CanOpen(i) {
			continue
		}
		for j := i + 2; j+1 < len(src); j++ {
			if src[j] != ':' || src[j+1] != ':' || !sx.CanClose(i, j) {
				continue
			}
			return src[i : j+2], true
		}
	}
	return "", false
}

// A slot never straddles a comment or a literal, which is the rule the whole
// scanner exists to enforce.
func TestRegionsContainTheirDelimiters(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"a slot inside a string is a slot",
			"msg := \":: the greeting ::\"\n",
			":: the greeting ::",
		},
		{
			"a `::` in a string cannot pair with one outside it",
			"a := \"::\"\nb := 1\n:: write the rest ::\n",
			":: write the rest ::",
		},
		{
			"a `::` in a comment cannot pair with one outside it",
			"// half a :: thought\nx := 1\n:: write the rest ::\n",
			":: write the rest ::",
		},
		{
			"a comment marker inside a string is not a comment",
			"u := \"http://x\" // :: fetch it ::\n",
			":: fetch it ::",
		},
		{
			"a quote inside a comment is not a string",
			"// it's the caller's job\n:: write the caller ::\n",
			":: write the caller ::",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := firstSlot(clike, tc.src)
			if !ok || got != tc.want {
				t.Errorf("slot = %q (%v), want %q", got, ok, tc.want)
			}
		})
	}
}

func TestCharLiteral(t *testing.T) {
	tests := []struct {
		src  string
		want int // length of the literal, 0 when there is none
	}{
		{"'a'", 3},
		{"'\\n'", 4},
		{`'\''`, 4}, // the escaped quote must not be left outside the literal
		{`'\\'`, 4},
		{"'\\u{1F600}'", 11},
		{"'日'", 5},
		{"'ab'", 0},   // too long to be one
		{"'s and", 0}, // an apostrophe
		{"'a", 0},     // a lifetime
	}
	for _, tc := range tests {
		end, ok := CharLiteral(tc.src, 0)
		if !ok {
			end = 0
		}
		if end != tc.want {
			t.Errorf("CharLiteral(%q) = %d, want %d", tc.src, end, tc.want)
		}
	}
}

func TestAnalyzeMarksRegions(t *testing.T) {
	src := "x := \"a\" // b\n"
	sx := clike.Analyze(src)
	for _, tc := range []struct {
		at   int
		want bool
	}{
		{0, false},  // x
		{5, true},   // the opening quote
		{6, true},   // a
		{8, false},  // the space between them
		{9, true},   // the comment
		{13, false}, // the newline, which the comment leaves out
	} {
		if got := sx.regionAt(tc.at) >= 0; got != tc.want {
			t.Errorf("regionAt(%d) in a region = %v, want %v (%q)", tc.at, got, tc.want, src[tc.at:tc.at+1])
		}
	}
}

// A nil language is Generic, so a caller that has not resolved one still gets
// the shared rules rather than a panic.
func TestNilLanguageIsGeneric(t *testing.T) {
	var l *Language
	sx := l.Analyze(":: hi ::")
	if sx.Language() != Generic {
		t.Errorf("Language() = %v, want Generic", sx.Language())
	}
	if !sx.CanOpen(0) {
		t.Error("CanOpen(0) = false, want the shared rules to apply")
	}
}

func TestInBracketsOnlyWhereAsked(t *testing.T) {
	if clike.Analyze("xs[1]").InBrackets(3) {
		t.Error("a language that never asked for brackets is tracking them")
	}
	if !subscripted.Analyze("xs[1]").InBrackets(3) {
		t.Error("a language asked for brackets and did not get them")
	}
	// Brackets written inside a string or a comment are text, not nesting.
	if subscripted.Analyze("s = \"[\"\nxs = 1\n").InBrackets(9) {
		t.Error("a bracket in a string opened a subscript")
	}
}
