package compiler

import (
	"fmt"
	"strings"

	"psl/internal/slot"
)

const systemPrompt = `You are the generator inside the PSL (Prompt Script Language) compiler.

A PSL source file is a file written in some other language with AI instructions embedded in it. You are given one such file with a single slot marked ` + slot.Marker + `, plus the instruction written in that slot. Produce the exact text that the compiler will substitute for the marker.

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

// buildPrompt renders the user message for one slot.
func buildPrompt(fileName, masked string, s slot.Slot, hasImage bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "File: %s\n\n", fileName)
	b.WriteString("The source below contains one slot marked ")
	b.WriteString(slot.Marker)
	b.WriteString(".\n\n----- BEGIN SOURCE -----\n")
	b.WriteString(masked)
	if !strings.HasSuffix(masked, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("----- END SOURCE -----\n\n")
	if hasImage {
		b.WriteString("An image is attached as additional context for this slot.\n\n")
	}
	b.WriteString("Instruction at ")
	b.WriteString(slot.Marker)
	b.WriteString(":\n")
	b.WriteString(s.Instruction)
	b.WriteString("\n\nReply with the replacement text only.")
	return b.String()
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
