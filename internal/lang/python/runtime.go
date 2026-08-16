package python

import (
	"fmt"
	"strconv"
	"strings"

	"psl/internal/lang"
)

// TranslateRuntime turns each Python slot into a subprocess expression that
// asks the psl executable to resolve it when Python reaches it. A slot may
// stand on its own as an expression or occupy an entire string literal. The
// latter includes f-strings, so current loop or request values can become part
// of every independently resolved instruction.
func TranslateRuntime(source string, sx *lang.Syntax, slots []lang.RuntimeSlot, opts lang.RuntimeOptions) (string, int, error) {
	for i := len(slots) - 1; i >= 0; i-- {
		s := slots[i]
		start, end := s.Start, s.End
		slotExpression := strconv.Quote(source[s.Start:s.End])

		if sx.InComment(s.Start) {
			return "", 0, fmt.Errorf("%s: runtime slot at %s is inside a comment; use it as a Python expression or as the whole contents of a string",
				opts.Path, sourcePosition(source, s.Start))
		}
		if stringStart, stringEnd, ok := sx.StringAt(s.Start); ok {
			literal := source[stringStart:stringEnd]
			only, bytes := literalContainsOnlySlot(literal, s.Start-stringStart, s.End-stringStart)
			if !only {
				return "", 0, fmt.Errorf("%s: runtime slot at %s must occupy the entire Python string literal",
					opts.Path, sourcePosition(source, s.Start))
			}
			if bytes {
				return "", 0, fmt.Errorf("%s: runtime slot at %s cannot use a bytes literal",
					opts.Path, sourcePosition(source, s.Start))
			}
			start, end = stringStart, stringEnd
			// Passing the original literal expression preserves Python escapes and
			// lets an f-string interpolate its current values.
			slotExpression = literal
		}

		call := runtimeCall(slotExpression, s.Start, opts)
		source = source[:start] + call + source[end:]
	}
	return source, len(slots), nil
}

func literalContainsOnlySlot(literal string, slotStart, slotEnd int) (only, bytes bool) {
	if slotStart < 0 || slotEnd < slotStart || slotEnd > len(literal) {
		return false, false
	}
	empty := literal[:slotStart] + literal[slotEnd:]
	i := 0
	for i < len(empty) && i < 2 && strings.ContainsRune("rRbBfFuU", rune(empty[i])) {
		i++
	}
	prefix, fences := empty[:i], empty[i:]
	switch fences {
	case `""`, `''`, `""""""`, `''''''`:
		return true, strings.ContainsAny(prefix, "bB")
	default:
		return false, false
	}
}

func runtimeCall(slotExpression string, slotStart int, opts lang.RuntimeOptions) string {
	parts := []string{strconv.Quote(opts.Executable), strconv.Quote("resolve"), slotExpression}
	if opts.SourcePath != "" {
		parts = append(parts,
			strconv.Quote("--context-file"), strconv.Quote(opts.SourcePath),
			strconv.Quote("--context-offset"), strconv.Quote(strconv.Itoa(slotStart)),
		)
	}
	if opts.Prompt != "" {
		parts = append(parts, strconv.Quote("--prompt"), strconv.Quote(opts.Prompt))
	}
	if opts.ImageMediaType != "" {
		image := "data:" + opts.ImageMediaType + ";base64," + opts.ImageBase64
		parts = append(parts, strconv.Quote("--image"), strconv.Quote(image))
	}
	return `__import__("subprocess").check_output([` + strings.Join(parts, ", ") + `], text=True)`
}

func sourcePosition(source string, offset int) string {
	line, column := 1, 1
	for i := 0; i < offset && i < len(source); i++ {
		if source[i] == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return fmt.Sprintf("line %d, column %d", line, column)
}
