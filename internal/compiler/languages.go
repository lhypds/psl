package compiler

// The languages psl has. Each folder under internal/lang registers itself with
// internal/lang when it is imported, and importing them is the one edit adding
// a language asks of the rest of psl.
//
// They are linked in here because the compiler is what asks which language a
// file is written in: a psl built without them would compile every file under
// the generic rules and warn about each one. Everything downstream — the usage
// text's list of languages, the parser, the prompt — reads the same table.
import (
	_ "psl/internal/lang/c"
	_ "psl/internal/lang/csharp"
	_ "psl/internal/lang/golang"
	_ "psl/internal/lang/javascript"
	_ "psl/internal/lang/macro"
	_ "psl/internal/lang/python"
	_ "psl/internal/lang/rust"
	_ "psl/internal/lang/typescript"
)
