package javascript

import (
	"testing"

	"psl/internal/langtest"
)

func TestJavaScript(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a function body",
			Src:  "function fib(n) {\n  :: fill in the loop ::\n}\n",
			Want: ":: fill in the loop ::",
		},
		{
			Name: "slot in a comment",
			Src:  "// :: one-line doc comment for fib ::\nfunction fib(n) {}\n",
			Want: ":: one-line doc comment for fib ::",
		},
		{
			Name: "a pseudo-element in a string",
			Src:  "el.querySelector('li::before');\n// :: style it ::\n",
			Want: ":: style it ::",
		},
		{
			Name: "a template literal is one region",
			Src:  "const tpl = `grid: ${name}::marker`;\n// :: render it ::\n",
			Want: ":: render it ::",
		},
		{
			// The one that needs real work: a slash is a pattern here, not
			// division.
			Name: "a regular expression",
			Src:  "const re = /^[a-f0-9:]+::1$/;\n// :: match the address ::\n",
			Want: ":: match the address ::",
		},
		{
			Name: "a regular expression after a keyword",
			Src:  "function f() { return /a::b/.test(s); }\n// :: explain it ::\n",
			Want: ":: explain it ::",
		},
		{
			Name: "a regular expression with a slash in a class",
			Src:  "const re = /[/:]::x/;\n// :: match it ::\n",
			Want: ":: match it ::",
		},
		{
			// Division must not swallow the rest of the line as a pattern.
			Name: "division is not a pattern",
			Src:  "const ratio = width / height;\n// :: clamp it ::\n",
			Want: ":: clamp it ::",
		},
		{
			Name: "an apostrophe in an instruction",
			Src:  ":: don't throw when the array is empty ::",
			Want: ":: don't throw when the array is empty ::",
		},
		{
			Name: "and/or in an instruction is not a pattern",
			Src:  ":: accept a string and/or a number ::",
			Want: ":: accept a string and/or a number ::",
		},
		{
			Name: "no slot",
			Src:  "export function fib(n) { return n; }\n",
			Want: "",
		},
	})
}
