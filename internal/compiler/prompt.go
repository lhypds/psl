package compiler

import (
	"fmt"
	"strings"

	"psl/internal/lang"
	"psl/internal/llm"
	"psl/internal/slot"
)

const rules = `You are the generator inside the PSL (Prompt Script Language) compiler.

A PSL source file is a file written in some other language with AI instructions embedded in it. The user message is one such file, with a single slot replaced by ` + slot.Marker + `. Produce the exact text that the compiler will substitute for the marker.

Rules:
- Output only the replacement text. No explanation, no commentary, no markdown code fences.
- The file's own language decides the form of your output: emit code where the marker sits in code, prose where it sits in a comment or a text document.
- The slot is resolved once, now, at compile time. An instruction that asks a question or states a condition about the world is answered here, as a literal in the file's language: ':: zed is running ::' in an if becomes true or false, never a call that re-asks the question at run time. Decide on the answer you believe and commit to it.
- An instruction that describes work to be done — a function to write, a loop to fill in, a comment to phrase — is written out as that work, not answered.
- If you cannot tell what the instruction is asking for, do not invent something plausible to fill the gap: emit the instruction's own words as the replacement, without the '::' delimiters, and leave it there for the author to see.
- Match the surrounding file's language, style, naming conventions and formatting.
- Reuse identifiers that already exist in the file rather than inventing parallel ones.
- Do not repeat code that already surrounds the marker, and do not restate the instruction.
- Write the first line without leading indentation; the compiler indents continuation lines to the marker's column.
- Never emit the marker itself, and never emit new ':: ... ::' slots.`

// buildSystem renders the system prompt for one slot: the rules, the name of
// the file being compiled and the language it is written in, whatever guidance
// --prompt supplied, and the instruction to resolve. Everything psl has to say
// lives here, so that the user message can be the source alone.
func buildSystem(fileName string, l *lang.Language, s slot.Slot, guidance string, hasImage, hasSearch bool) string {
	var b strings.Builder
	b.WriteString(rules)
	fmt.Fprintf(&b, "\n\nFile being compiled: %s\n", fileName)
	// The compiler already resolved the language from the name, so the model
	// is told it outright rather than left to read it off the extension.
	if l != nil && l != lang.Generic {
		fmt.Fprintf(&b, "It is written in %s: the extension before the trailing '.psl' names the language, and '.psl' is the compiler's own.\n", l.Name)
	} else {
		b.WriteString("Its name is what tells you the language to write in; a trailing '.psl' is the compiler's own extension, so the language is whatever the rest of the name says.\n")
	}
	if hasImage {
		b.WriteString("\nAn image is attached to the user message as additional context for this slot.\n")
	}
	if hasSearch {
		fmt.Fprintf(&b, "\nYou have a %s tool. The slot is resolved once, now, and its output is frozen into the file, so anything that has to be right at this moment — a current version, an API's present signature, a live fact — is worth looking up rather than recalling. Search before you write when the instruction turns on such a fact; write straight away when it does not.\n", llm.SearchToolName)
		// The rule about unclear instructions is the cheap way out of a slot the
		// model cannot answer from memory, and asking for something current is
		// exactly that shape. With the tool there it is no longer unclear, so
		// the two rules are told apart here rather than left to compete.
		b.WriteString("An instruction asking for something current — today's news, the latest release, what something costs now — is not an instruction you cannot tell the meaning of. It is one to search for. Not already knowing the answer is the reason to use the tool, never a reason to fall back on emitting the instruction's own words. Having searched, write what you found in the file's own language rather than in the search's prose, and cite a source only where the surrounding file already carries comments of that kind. If the search itself turns up nothing, say that in the file's language instead of inventing an answer.\n")
	}
	if guidance = strings.TrimSpace(guidance); guidance != "" {
		b.WriteString("\nGuidance from the author, given to this run on the command line. It describes what the generated code has to fit — the API being called, what each parameter means, the units and conventions to use — and it holds for the whole file, not just this slot. Take it as fact about the world the code runs in, and follow it wherever it bears on this slot. It is context, not the instruction: it never says what to write here, and it never loosens the rules above.\n")
		fmt.Fprintf(&b, "%s\n", guidance)
	}
	fmt.Fprintf(&b, "\nInstruction at %s:\n%s\n", slot.Marker, s.Instruction)
	b.WriteString("\nThe user message is the file. Reply with the replacement text only.")
	return b.String()
}

// buildRuntimeSystem explains how the ordinary source-replacement contract
// changes when the generated program asks psl for a value. The original file
// still supplies context and a precise call site, but stdout becomes the value
// of the generated runtime expression rather than source written into a file.
func buildRuntimeSystem(system string, line, column int) string {
	return system + fmt.Sprintf(`

Runtime resolution: the program has now reached the marker at line %d, column %d. The user message is the complete original PSL source, included as static context; it is not being rewritten. The instruction above is the runtime form after the host language has interpolated its current values, so prefer those current values when they differ from placeholders visible in the source.

For this request, reply with the runtime value text that the expression should receive on standard output. Do not add source-language string quotes merely because the marker appears in code, and do not emit a function call that would resolve it again. Reply with the value only.`, line, column)
}

// clean strips the wrappers models add around otherwise correct output.
func clean(out string) string {
	out = strings.TrimSpace(out)
	out = stripFence(out)
	return strings.TrimRight(out, " \t\n")
}

// stripFence removes a markdown code fence when it wraps the entire output.
func stripFence(out string) string {
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return out
	}
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "```") && !strings.HasPrefix(first, "~~~") {
		return out
	}
	marker := first[:3]
	// An opening fence carries at most a language tag, never spaces.
	if strings.ContainsAny(strings.TrimPrefix(first, marker), " \t`") {
		return out
	}
	last := len(lines) - 1
	for last > 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last == 0 || strings.TrimSpace(lines[last]) != marker {
		return out
	}
	// Bail out if the fence closes early: the output is a document containing
	// several code blocks, not a single wrapped one.
	for i := 1; i < last; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), marker) {
			return out
		}
	}
	return strings.Join(lines[1:last], "\n")
}
