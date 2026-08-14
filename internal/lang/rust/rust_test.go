package rust

import (
	"testing"

	"psl/internal/langtest"
)

func TestRust(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "slot in a function body",
			Src:  "fn main() {\n    :: print the greeting ::\n}\n",
			Want: ":: print the greeting ::",
		},
		// Paths, in every shape Rust writes them.
		{Name: "use declaration", Src: "use std::io::Write;\n", Want: ""},
		{Name: "associated function", Src: "let v = Vec::new();\n", Want: ""},
		{Name: "turbofish", Src: "let v = Vec::<i32>::new();\n", Want: ""},
		{Name: "qualified path", Src: "let d = <T as Default>::default();\n", Want: ""},
		{
			Name: "a turbofish cannot close an unfinished instruction",
			Src:  ":: build the vector\nlet v = Vec::<i32>::new();\n",
			Want: "",
		},
		{
			Name: "a qualified path cannot open one",
			Src:  "let d = <T as Default>::default();\nlet e = 1;\n",
			Want: "",
		},
		{
			Name: "a path inside an instruction",
			Src:  ":: write it with std::io::Write ::",
			Want: ":: write it with std::io::Write ::",
		},
		{
			// A lifetime is a quote with nothing closing it one character
			// later, so it never starts a literal.
			Name: "lifetimes",
			Src:  "fn head<'a>(xs: &'a [u8]) -> &'a u8 {\n    :: return the first byte ::\n}\n",
			Want: ":: return the first byte ::",
		},
		{
			Name: "a raw string fenced with hashes",
			Src:  "let s = r#\"say \"::1\" here\"#;\n// :: parse it ::\n",
			Want: ":: parse it ::",
		},
		{
			Name: "nested block comments",
			Src:  "/* outer /* inner :: not a slot */ */\n// :: the real one ::\n",
			Want: ":: the real one ::",
		},
		{
			Name: "an apostrophe in an instruction",
			Src:  ":: don't panic on an empty slice ::",
			Want: ":: don't panic on an empty slice ::",
		},
		{
			Name: "no slot",
			Src:  "fn main() {\n    println!(\"{}\", std::u8::MAX);\n}\n",
			Want: "",
		},
	})
}
