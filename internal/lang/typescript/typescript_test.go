package typescript

import (
	"testing"

	"psl/internal/langtest"
)

// TypeScript reads the same as JavaScript, so its own tests only have to prove
// the wiring — and that the type language brings no `::` with it.
func TestTypeScript(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a typed function",
			Src:  "function fib(n: number): number {\n  :: fill in the loop ::\n}\n",
			Want: ":: fill in the loop ::",
		},
		{
			Name: "a generic signature is not a slot",
			Src:  "function head<T>(xs: T[]): T | undefined { return xs[0]; }\n",
			Want: "",
		},
		{
			Name: "a regular expression",
			Src:  "const re: RegExp = /^a::b$/;\n// :: match it ::\n",
			Want: ":: match it ::",
		},
		{
			Name: "an index signature is not a slot",
			Src:  "type M = { [k: string]: number };\n",
			Want: "",
		},
	})
}
