Languages
---------

PSL marks an AI instruction with `::`, and `::` already means something in most languages. Which `::` in a file is a delimiter and which is the language's own syntax is not a question that can be answered in general — it depends on the language — so psl does not try. Every rule lives in [internal/lang](../internal/lang), one folder per language, and the file's name is what says which folder to read it under.


Naming

```text
fib.go.psl        Go
bot.py.psl        Python
Program.cs.psl    C#
main.c.psl        C
lib.rs.psl        Rust
app.ts.psl        TypeScript
```

The extension before `.psl` selects the language. A name that does not carry one — `fib.psl` — is refused, because there is nothing to select with:

```shell
$ psl fib.psl
psl: fib.psl: no language in the name: a PSL file is named <name>.<language>.psl, …
```

An extension no language claims still compiles, under the generic rules, and says so once per run:

```shell
$ psl app.zig.psl
psl: warning: no rules for .zig, using the generic rules
```


What Every Language Shares

Two rules hold with or without a language of one's own, because scope resolution always glues `::` to an identifier:

- an opening `::` must not be glued to an identifier on its left;
- a closing `::` must not be glued to an identifier on its right.

That is the whole of the generic rules, and it is where each language folder starts.

Every language adds the same two things on top: what its comments look like, and what its literals look like. Neither is about `::` directly — they are about knowing where the text of the file is text. A slot never straddles a comment or a literal, so a `::` inside a string pairs only with a `::` in the same string:

```go
addr := "::1"                  // an address, not an opening delimiter
msg  := ":: say hello ::"      // a slot, resolved inside the string
```

A literal that is never closed is not treated as one. That is what lets instructions be written in English:

```python
:: don't crash when the list is empty ::
```

The apostrophe in `don't` finds no partner before the end of the line, so it stays an apostrophe rather than opening a string over the closing `::`.


What Each Language Adds

| Language | Folder | What it is for |
| --- | --- | --- |
| C | [c](../internal/lang/c) | Strings with encoding prefixes (`L"…"`, `u8"…"`), character literals. C23 attributes (`[[gnu::always_inline]]`) glue on both sides, so the shared rule has them. |
| C# | [csharp](../internal/lang/csharp) | Verbatim strings (`@"…"`, quote doubled to escape, newlines allowed), raw strings (`"""…"""`), interpolation prefixes. `global::System` glues on both sides. |
| Go | [golang](../internal/lang/golang) | Raw strings in backticks, which run over newlines and have no escapes. Go has no `::` of its own at all. The folder is `golang` because `go` is a keyword and cannot name a package. |
| JavaScript | [javascript](../internal/lang/javascript) | Template literals, and telling a regular expression from division — `/^[a-f0-9:]+::1$/` is the case that needs it. |
| Macro PSL | [macro](../internal/lang/macro) | Pob's macro language, `main.macro.psl`. It has no `::` of its own — every one in a macro is a slot — so all the folder is for is the double-quoted string a `typeText("ping ::1")` puts an address in. |
| Python | [python](../internal/lang/python) | Slices: `xs[::-1]`, `xs[::2]`, `xs[1::2]`. Triple-quoted strings and the `r`/`b`/`f`/`u` prefixes. |
| Rust | [rust](../internal/lang/rust) | Turbofish (`Vec::<i32>`), qualified paths (`<T as Default>::default()`), lifetimes that look like character literals (`&'a str`), raw strings fenced with hashes, nested block comments. |
| TypeScript | [typescript](../internal/lang/typescript) | JavaScript's rules, reached by importing them. The type language brings no `::` with it, and this folder says so on purpose, so a TypeScript-only rule later has somewhere to go. |

Beside the folders sit the pieces they are built from: [lang.go](../internal/lang/lang.go) resolves a name to a language and holds the table they register in, [syntax.go](../internal/lang/syntax.go) reads a source file into comments and literals, and [generic.go](../internal/lang/generic.go) is the shared rule above. None of the three is a language.

Python's slices are the one place a rule is not exact. A slice writes its bounds straight against the colons and a slot does not, so inside `[` … `]` an opening `::` needs whitespace after it and a closing `::` needs whitespace before it. `xs[:: the index of the largest ::]` works and `xs[::-1]` is left alone; a slice spaced out on both sides — `xs[a :: b]` — would be read as a delimiter, and should be written the ordinary way.


Adding a Language

One folder, and one import:

```go
// Package elixir holds Elixir's rules.
//
// Elixir writes `::` in bitstring sizes, <<n::8>>, which …
package elixir

import "psl/internal/lang"

// Language is Elixir: …
var Language = lang.Register(&lang.Language{
	Name:    "Elixir",
	Exts:    []string{"ex", "exs"},
	Comment: comment,
	String:  stringLiteral,
	Closes:  closes,
})
```

- `Name` is the language as a person writes it. It is what psl reports and what the model is told the file is written in.
- `Exts` are the extensions that select it. Two folders claiming one extension panics on load, so a collision cannot reach a user.
- `Comment` and `String` return the offset just past the construct starting at `i`. [syntax.go](../internal/lang/syntax.go) has the pieces to build them out of — `LineComment`, `BlockComment`, `Quoted`, `CharLiteral`, `CComments` — and the other language folders are worked examples.
- `Brackets` asks the scanner to track `[` … `]` nesting, which only Python needs.
- `Opens` and `Closes` veto a `::` the shared rules would have accepted. They run last, so a language folder only ever states what is peculiar to it.

A folder registers itself when it is imported, so the new language also needs a line in [internal/compiler/languages.go](../internal/compiler/languages.go), which is where psl links the set of them in. Without it the folder is a language psl does not have, and a `.ex.psl` file would compile under the generic rules.

Then write `<language>_test.go` beside it. The tests are a table of source and the slot span it must produce — `langtest.Run` from [internal/langtest](../internal/langtest) — so a new language is tested the same way as the others: every shape of `::` the language itself writes, and an instruction with an apostrophe in it.

```go
package elixir

import (
	"testing"

	"psl/internal/langtest"
)

func TestElixir(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "a bitstring size is not a slot",
			Src:  "<<n::8>>\n",
			Want: "",
		},
	})
}
```
