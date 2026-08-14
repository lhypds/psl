package python

import (
	"testing"

	"psl/internal/langtest"
)

func TestPython(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a function body",
			Src:  "def click_ok():\n    :: move to the OK button ::\n",
			Want: ":: move to the OK button ::",
		},
		{
			Name: "slot in a comment",
			Src:  "# :: docstring for fib ::\ndef fib(n): ...\n",
			Want: ":: docstring for fib ::",
		},
		// The slices. None of them may open or close anything, on their own or
		// in front of a real slot further down the file.
		{Name: "reversal", Src: "rev = xs[::-1]\n", Want: ""},
		{Name: "step", Src: "evens = xs[::2]\n", Want: ""},
		{Name: "whole", Src: "copy = xs[::]\n", Want: ""},
		{Name: "start and step", Src: "odds = xs[1::2]\n", Want: ""},
		{Name: "named bounds", Src: "part = xs[lo::step]\n", Want: ""},
		{
			Name: "a slice does not reach the slot below it",
			Src:  "rev = xs[::-1]\n# :: sum them ::\n",
			Want: ":: sum them ::",
		},
		{
			Name: "an unfinished instruction cannot close on a slice",
			Src:  ":: reverse the list\nrev = xs[::-1]\n",
			Want: "",
		},
		{
			// A slot inside a subscript still works: it has the spacing a
			// slice never has.
			Name: "a slot inside a subscript",
			Src:  "best = xs[:: the index of the largest ::]\n",
			Want: ":: the index of the largest ::",
		},
		{
			Name: "an apostrophe in an instruction",
			Src:  ":: don't crash when xs is empty ::",
			Want: ":: don't crash when xs is empty ::",
		},
		{
			Name: "two apostrophes in an instruction",
			Src:  ":: use the file's name, not the user's ::",
			Want: ":: use the file's name, not the user's ::",
		},
		{
			Name: "a quoted address stays quoted",
			Src:  "host = '::1'\n# :: bind to it ::\n",
			Want: ":: bind to it ::",
		},
		{
			Name: "a triple-quoted string is one region",
			Src:  "DOC = \"\"\"\nuse ::1 for the loopback\n\"\"\"\n# :: parse it ::\n",
			Want: ":: parse it ::",
		},
		{
			Name: "a prefixed string is a string",
			Src:  "pat = rb'a::b'\n# :: compile it ::\n",
			Want: ":: compile it ::",
		},
		{
			// The r here ends a name; the string beside it starts at its quote.
			Name: "a name ending in a prefix letter",
			Src:  "ptr'::1'\n# :: fix this ::\n",
			Want: ":: fix this ::",
		},
		{
			Name: "no slot",
			Src:  "def f():\n    return xs[::-1]\n",
			Want: "",
		},
	})
}
