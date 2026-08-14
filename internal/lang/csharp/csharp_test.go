package csharp

import (
	"testing"

	"psl/internal/langtest"
)

func TestCSharp(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a method body",
			Src:  "public int Fib(int n)\n{\n    :: fill in the loop ::\n}\n",
			Want: ":: fill in the loop ::",
		},
		{
			Name: "slot in a comment",
			Src:  "// :: one-line summary for Fib ::\npublic int Fib(int n) => 0;\n",
			Want: ":: one-line summary for Fib ::",
		},
		{
			Name: "the namespace alias qualifier",
			Src:  "var t = global::System.DateTime.Now;\nvar u = Alias::Widget.Make();\n",
			Want: "",
		},
		{
			Name: "an address in a string stays in the string",
			Src:  "var addr = \"::1\";\n// :: bind to it ::\n",
			Want: ":: bind to it ::",
		},
		{
			// A verbatim string escapes its quote by doubling it, so reading
			// it as an ordinary one would end it in the wrong place.
			Name: "a verbatim string with a doubled quote",
			Src:  "var p = @\"C:\\logs\\\"\"old::1\"\"\";\n// :: read it ::\n",
			Want: ":: read it ::",
		},
		{
			Name: "a verbatim string over several lines",
			Src:  "var q = @\"select *\nfrom t -- ::1\n\";\n// :: run it :: \n",
			Want: ":: run it ::",
		},
		{
			Name: "a raw string",
			Src:  "var j = \"\"\"{\"addr\": \"::1\"}\"\"\";\n// :: parse it ::\n",
			Want: ":: parse it ::",
		},
		{
			Name: "an interpolated string",
			Src:  "var s = $\"{host}::{port}\";\n// :: log it ::\n",
			Want: ":: log it ::",
		},
		{
			// @name is a verbatim identifier, not the start of a string.
			Name: "a verbatim identifier",
			Src:  "var @class = 1;\n// :: name it better ::\n",
			Want: ":: name it better ::",
		},
		{
			Name: "an apostrophe in an instruction",
			Src:  ":: don't throw when the list is empty ::",
			Want: ":: don't throw when the list is empty ::",
		},
		{
			Name: "no slot",
			Src:  "namespace App;\n\npublic class Program { }\n",
			Want: "",
		},
	})
}
