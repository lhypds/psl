package c

import (
	"testing"

	"psl/internal/langtest"
)

func TestC(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a function body",
			Src:  "int main(void) {\n    :: print the greeting ::\n}\n",
			Want: ":: print the greeting ::",
		},
		{
			Name: "slot in a comment",
			Src:  "/* :: what fib does, one line :: */\nint fib(int n);\n",
			Want: ":: what fib does, one line ::",
		},
		{
			Name: "an address in a string stays in the string",
			Src:  "const char *loopback = \"::1\";\n/* :: bind to it :: */\n",
			Want: ":: bind to it ::",
		},
		{
			Name: "a wide string is a string",
			Src:  "const wchar_t *w = L\"::1\";\n// :: bind to it ::\n",
			Want: ":: bind to it ::",
		},
		{
			Name: "a utf-8 string is a string",
			Src:  "const char *u = u8\"a::b\";\n// :: split it ::\n",
			Want: ":: split it ::",
		},
		{
			// C23 borrowed C++'s attributes, and they glue `::` to identifiers
			// on both sides.
			Name: "an attribute",
			Src:  "[[gnu::always_inline]] static void f(void) {}\n",
			Want: "",
		},
		{
			Name: "an apostrophe in an instruction",
			Src:  ":: don't overflow the buffer ::",
			Want: ":: don't overflow the buffer ::",
		},
		{
			Name: "a character literal in an instruction",
			Src:  ":: split on ',' and trim ::",
			Want: ":: split on ',' and trim ::",
		},
		{
			Name: "an unterminated block comment is not a comment",
			Src:  ":: wrap it in /* here ::",
			Want: ":: wrap it in /* here ::",
		},
		{
			Name: "no slot",
			Src:  "#include <stdio.h>\n\nint main(void) { return 0; }\n",
			Want: "",
		},
	})
}
