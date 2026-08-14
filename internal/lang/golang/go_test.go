package golang

import (
	"testing"

	"psl/internal/langtest"
)

func TestGo(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a comment",
			Src:  "// :: one-line doc comment for Fib ::\nfunc Fib(n int) int {}\n",
			Want: ":: one-line doc comment for Fib ::",
		},
		{
			Name: "slot in a function body",
			Src:  "func Fib(n int) int {\n\t:: fill in the loop ::\n}\n",
			Want: ":: fill in the loop ::",
		},
		{
			// The case Generic gets wrong: the address is a string, and the
			// slot below it is a slot.
			Name: "an address in a string stays in the string",
			Src:  "addr := \"::1\"\n// :: write the listener ::\n",
			Want: ":: write the listener ::",
		},
		{
			Name: "a raw string is a string too",
			Src:  "tmpl := `dial ::1 first`\n// :: write the dialer ::\n",
			Want: ":: write the dialer ::",
		},
		{
			Name: "a slot written inside a string still resolves",
			Src:  "msg := \":: a friendly greeting ::\"\n",
			Want: ":: a friendly greeting ::",
		},
		{
			// A quote in a comment opens nothing, so the slot below is found
			// where it is.
			Name: "a lone quote in a comment",
			Src:  "// the file's name\n// :: open it ::\n",
			Want: ":: open it ::",
		},
		{
			// An apostrophe is not a rune literal: a rune holds one character
			// and then closes.
			Name: "an apostrophe in an instruction",
			Src:  ":: don't crash when the list is empty ::",
			Want: ":: don't crash when the list is empty ::",
		},
		{
			Name: "a rune literal in an instruction",
			Src:  ":: split on ',' and trim ::",
			Want: ":: split on ',' and trim ::",
		},
		{
			Name: "a slot may not reach out of a comment",
			Src:  "// a note about :: something\nx := 1\n:: write more ::\n",
			Want: ":: write more ::",
		},
		{
			Name: "no slot",
			Src:  "package main\n\nfunc main() {}\n",
			Want: "",
		},
	})
}
