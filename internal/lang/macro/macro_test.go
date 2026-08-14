package macro

import (
	"testing"

	"psl/internal/langtest"
)

func TestMacro(t *testing.T) {
	langtest.Run(t, Language, []langtest.Case{
		{
			Name: "a slot as a whole statement",
			Src:  "move(398, 915)\n:: click the OK button ::\n",
			Want: ":: click the OK button ::",
		},
		{
			Name: "a slot as an argument",
			Src:  "move(:: the x offset to the message box ::, 738)\n",
			Want: ":: the x offset to the message box ::",
		},
		{
			Name: "a slot inside a string",
			Src:  "typeText(\":: a short reply to the message on screen ::\")\n",
			Want: ":: a short reply to the message on screen ::",
		},
		{
			// The reason this folder exists: the address is text the macro
			// types, and the slot below it is the slot.
			Name: "an address in a string stays in the string",
			Src:  "typeText(\"ping ::1 first\")\n// :: wait for the reply ::\n",
			Want: ":: wait for the reply ::",
		},
		{
			Name: "two strings cannot pair with each other",
			Src:  "typeText(\"ping ::1\")\ntypeText(\"then ::ffff:0:0\")\n",
			Want: "",
		},
		{
			// A backslash escapes the quote after it, so the string does not
			// end early and what it holds stays inside it.
			Name: "an escaped quote does not end the string",
			Src:  "typeText(\"say \\\"::1\\\"\")\n// :: press return ::\n",
			Want: ":: press return ::",
		},
		{
			// Macro PSL closes an unclosed string at the parenthesis and writes
			// the quote back, so the slot in one is a slot psl has to fill.
			Name: "a slot in an unclosed string",
			Src:  "typeText(\"::what to say::)\n",
			Want: "::what to say::",
		},
		{
			Name: "slot in a line comment",
			Src:  "// :: what this macro does, one line ::\nmove(398, 915)\n",
			Want: ":: what this macro does, one line ::",
		},
		{
			Name: "slot in a block comment",
			Src:  "/* :: what this macro does, one line :: */\nclick()\n",
			Want: ":: what this macro does, one line ::",
		},
		{
			Name: "a slot may not reach out of a comment",
			Src:  "// a note about :: something\nclick()\n:: type the reply ::\n",
			Want: ":: type the reply ::",
		},
		{
			Name: "an apostrophe in an instruction",
			Src:  ":: don't click twice when the dialog is already open ::",
			Want: ":: don't click twice when the dialog is already open ::",
		},
		{
			Name: "a condition holding a slot",
			Src:  "if (:: the window focus on a wechat user ::) {\n    click()\n}\n",
			Want: ":: the window focus on a wechat user ::",
		},
		{
			Name: "no slot",
			Src:  "move(398, 915)\nclick()\nkeyPress(\"cmd+v\")\nsleep(250ms)\n",
			Want: "",
		},
	})
}
