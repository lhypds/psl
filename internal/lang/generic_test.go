package lang_test

import (
	"testing"

	"psl/internal/lang"
	"psl/internal/langtest"
)

// The generic rules are what psl falls back to, and what every language folder
// starts from: the identifier glue, and nothing else.
func TestGeneric(t *testing.T) {
	langtest.Run(t, lang.Generic, []langtest.Case{
		{
			Name: "plain slot",
			Src:  "hello :: say hi :: world",
			Want: ":: say hi ::",
		},
		{
			Name: "no whitespace",
			Src:  "if (::zed is running::) {}",
			Want: "::zed is running::",
		},
		{
			Name: "multiline slot",
			Src:  "x = ::\n  write a parser\n::\n",
			Want: "::\n  write a parser\n::",
		},
		{
			Name: "scope resolution",
			Src:  "std::cout << x;\nstd::vector<int> v;\n",
			Want: "",
		},
		{
			Name: "scope resolution inside an instruction",
			Src:  ":: print with std::cout ::",
			Want: ":: print with std::cout ::",
		},
		{
			Name: "identifier glued on the left",
			Src:  "foo:: bar ::",
			Want: "",
		},
		{
			Name: "unterminated",
			Src:  ":: never closed",
			Want: "",
		},
		{
			// The price of optional whitespace, and the reason a language
			// folder is worth writing: with nothing known about the file, a
			// `::` followed by a space is a closing delimiter.
			Name: "scope resolution followed by a space closes early",
			Src:  ":: fix the std:: usage ::",
			Want: ":: fix the std::",
		},
		{
			// Generic knows no literals, so a string cannot protect anything.
			// This is the case the language folders exist to get right.
			Name: "a string is not a barrier",
			Src:  "addr = \"::1\"\n:: write the listener ::\n",
			Want: "::1\"\n::",
		},
	})
}
