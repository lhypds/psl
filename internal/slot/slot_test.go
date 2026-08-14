package slot

import (
	"testing"

	"psl/internal/lang"
	"psl/internal/lang/golang"
	"psl/internal/lang/python"
)

func TestFind(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		lang        *lang.Language // nil is lang.Generic
		found       bool
		model       string
		instruction string
		indent      string
		span        string
	}{
		{
			name:        "plain slot",
			src:         "hello :: say hi :: world",
			found:       true,
			instruction: "say hi",
			span:        ":: say hi ::",
		},
		{
			name:        "model prefix",
			src:         ":: gpt-5.6> say hi ::",
			found:       true,
			model:       "gpt-5.6",
			instruction: "say hi",
			span:        ":: gpt-5.6> say hi ::",
		},
		{
			name:        "slash in model name",
			src:         ":: openrouter/anthropic/claude-opus-5> hi ::",
			found:       true,
			model:       "openrouter/anthropic/claude-opus-5",
			instruction: "hi",
			span:        ":: openrouter/anthropic/claude-opus-5> hi ::",
		},
		{
			name:        "greater-than in prose is not a model",
			src:         ":: assert that a > b ::",
			found:       true,
			instruction: "assert that a > b",
			span:        ":: assert that a > b ::",
		},
		{
			name:        "multiline slot",
			src:         "x = ::\n  write a parser\n  in Go\n::\n",
			found:       true,
			instruction: "write a parser\n  in Go",
			span:        "::\n  write a parser\n  in Go\n::",
		},
		{
			name:        "indented slot records its indent",
			src:         "func f() {\n\t:: return 42 ::\n}\n",
			found:       true,
			instruction: "return 42",
			indent:      "\t",
			span:        ":: return 42 ::",
		},
		{
			name:        "no whitespace inside the delimiters",
			src:         "if (::zed is running::) {\n    click(40, 40)\n}\n",
			found:       true,
			instruction: "zed is running",
			span:        "::zed is running::",
		},
		{
			name:        "no whitespace on one side only",
			src:         "if (::zed is running ::) {\n}\n",
			found:       true,
			instruction: "zed is running",
			span:        "::zed is running ::",
		},
		{
			name:        "model prefix without whitespace after the delimiter",
			src:         "::gpt-5.6> zed is running::",
			found:       true,
			model:       "gpt-5.6",
			instruction: "zed is running",
			span:        "::gpt-5.6> zed is running::",
		},
		{
			name:  "cpp scope resolution is not a slot",
			src:   "std::cout << x;\nstd::vector<int> v;\n",
			found: false,
		},
		{
			name:  "rust and php scope resolution are not slots",
			src:   "let v = Foo::Bar::new();\nself::method();\n",
			found: false,
		},
		{
			name:        "cpp scope resolution inside a slot body",
			src:         ":: print with std::cout ::",
			found:       true,
			instruction: "print with std::cout",
			span:        ":: print with std::cout ::",
		},
		{
			name:  "identifier glued to the left is not a slot",
			src:   "foo:: bar ::",
			found: false,
		},
		{
			// The price of optional whitespace: `std::` followed by a space is
			// indistinguishable from a closing delimiter. Written the normal
			// way, `std::cout`, it stays inside the instruction.
			name:        "scope resolution followed by a space closes the slot early",
			src:         ":: fix the std:: usage ::",
			found:       true,
			instruction: "fix the std",
			span:        ":: fix the std::",
		},
		{
			name:        "punctuation on the left still opens a slot",
			src:         "print(:: greet ::)",
			found:       true,
			instruction: "greet",
			span:        ":: greet ::",
		},
		{
			name:  "unterminated slot",
			src:   ":: never closed",
			found: false,
		},
		{
			name:  "ruby symbol array is not a slot",
			src:   "x = a ? b :: c",
			found: false,
		},
		// The language decides which `::` are its own syntax; the parser only
		// asks. These two files are identical apart from the rules read over
		// them. internal/lang holds the exhaustive per-language cases.
		{
			name:        "the language rules out its own syntax",
			src:         "rev = xs[::-1]\n# :: sum them ::\n",
			lang:        python.Language,
			found:       true,
			instruction: "sum them",
			span:        ":: sum them ::",
		},
		{
			name:        "without a language the same file reads differently",
			src:         "rev = xs[::-1]\n# :: sum them ::\n",
			found:       true,
			instruction: "-1]\n#",
			span:        "::-1]\n# ::",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Find(tc.src, tc.lang)
			if ok != tc.found {
				t.Fatalf("Find() found = %v, want %v (slot %+v)", ok, tc.found, got)
			}
			if !ok {
				return
			}
			if got.Model != tc.model {
				t.Errorf("Model = %q, want %q", got.Model, tc.model)
			}
			if got.Instruction != tc.instruction {
				t.Errorf("Instruction = %q, want %q", got.Instruction, tc.instruction)
			}
			if got.Indent != tc.indent {
				t.Errorf("Indent = %q, want %q", got.Indent, tc.indent)
			}
			if span := tc.src[got.Start:got.End]; span != tc.span {
				t.Errorf("span = %q, want %q", span, tc.span)
			}
		})
	}
}

func TestFindReturnsFirstSlot(t *testing.T) {
	src := "a :: one :: b :: two ::"
	s, ok := Find(src, nil)
	if !ok {
		t.Fatal("Find() found no slot")
	}
	if s.Instruction != "one" {
		t.Fatalf("Instruction = %q, want %q", s.Instruction, "one")
	}
}

func TestCount(t *testing.T) {
	tests := []struct {
		src  string
		lang *lang.Language
		want int
	}{
		{src: "", want: 0},
		{src: "std::cout", want: 0},
		{src: ":: one ::", want: 1},
		{src: ":: one :: and :: two :: and :: three ::", want: 3},
		// Counting reads the file under the same rules as finding, so a slice
		// is no more a slot to Count than it is to Find.
		{src: "xs[::-1]\n:: one ::\nys[::2]\n:: two ::\n", lang: python.Language, want: 2},
	}
	for _, tc := range tests {
		if got := Count(tc.src, tc.lang); got != tc.want {
			t.Errorf("Count(%q) = %d, want %d", tc.src, got, tc.want)
		}
	}
}

func TestReplace(t *testing.T) {
	t.Run("inline", func(t *testing.T) {
		src := "x := :: the answer ::\n"
		s, _ := Find(src, golang.Language)
		if got, want := Replace(src, s, "42"), "x := 42\n"; got != want {
			t.Errorf("Replace() = %q, want %q", got, want)
		}
	})

	t.Run("indents continuation lines", func(t *testing.T) {
		src := "func f() int {\n\t:: return the answer ::\n}\n"
		s, _ := Find(src, golang.Language)
		got := Replace(src, s, "x := 42\n\nreturn x")
		want := "func f() int {\n\tx := 42\n\n\treturn x\n}\n"
		if got != want {
			t.Errorf("Replace() = %q, want %q", got, want)
		}
	})

	t.Run("no reindent when the slot is not first on its line", func(t *testing.T) {
		src := "\tx := :: two lines ::\n"
		s, _ := Find(src, golang.Language)
		got := Replace(src, s, "a\nb")
		want := "\tx := a\nb\n"
		if got != want {
			t.Errorf("Replace() = %q, want %q", got, want)
		}
	})
}

func TestMask(t *testing.T) {
	src := "before :: do it :: after"
	s, _ := Find(src, nil)
	if got, want := Mask(src, s), "before "+Marker+" after"; got != want {
		t.Errorf("Mask() = %q, want %q", got, want)
	}
}
