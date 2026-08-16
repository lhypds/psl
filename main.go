// Command psl compiles a Prompt Script Language file: it resolves the first
// remaining `:: ... ::` slot with an AI model and writes the result back into
// the file in place of the slot. Run it repeatedly until no slots remain.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"psl/internal/compiler"
	"psl/internal/editor"
	"psl/internal/imageref"
	"psl/internal/lang"
	"psl/internal/llm"
	"psl/internal/psllog"
	"psl/internal/pslrc"
	"psl/internal/updater"
)

// versionFile is the released version, carried in the binary so that psl
// reports it however it was built. release.sh tags "v" + this.
//
//go:embed VERSION
var versionFile string

// exampleFile is what `psl config` writes when there is no .pslrc to edit yet,
// so the editor opens a file to fill in rather than an empty buffer. It is the
// same example the release ships alongside the binary.
//
//go:embed .pslrc.example
var exampleFile string

// version is the exact build, stamped in by build.sh with
// -ldflags "-X main.version=...". It is shown alongside the released version
// when the two differ, which is what identifies a development build.
var version = ""

// usage names the languages psl has a folder for, so the list can never
// drift from internal/lang.
var usage = fmt.Sprintf(usageTemplate, languageList())

const usageTemplate = `psl — Prompt Script Language compiler

Usage:
  psl <file.psl> [--image <base64_image>] [--prompt <text>]
  psl run <file.psl> [--image <base64_image>] [--prompt <text>] [-- <args>...]
  psl config
  psl usage
  psl update

A plain psl <file.psl> invocation resolves exactly one AI slot: psl finds the
first remaining :: instruction ::, generates its output, and writes the result
back over the slot. Invoke it again for the next slot.

psl run translates without changing the input, writes the generated language
file beside it without the trailing .psl, then invokes that language's executor.
Python slots resolve whenever execution reaches them. Arguments after -- are
passed to the generated program.

A PSL file is named <name>.<language>.psl — main.py.psl, Program.cs.psl,
fib.go.psl. The extension before .psl says which language's own syntax psl
must not read as a slot; an extension no language of psl's claims compiles
under the generic rules.

Languages: %s.

Commands:
  run <file.psl>        translate, write <file>, and execute it
  config               edit .pslrc in $VISUAL, $EDITOR, or vim
  usage                tokens spent per model, totalled from ~/.psl/psl.log
  update               replace this executable with the latest GitHub release

Options:
  -i, --image <data>   image given to the slot(s) resolved by this invocation;
                       accepts a file path, a data: URL, or raw base64
  -p, --prompt <text>  guidance added to the system prompt: the API the code
                       has to fit, what each parameter means, in what units;
                       accepts the text itself or a path to a text file
  -h, --help           show this help
  -v, --version        show the version

Configuration lives in .pslrc, looked up in the current directory and then in
your home directory. It is optional: without it psl uses OPENAI_API_KEY if that
is set, otherwise ANTHROPIC_API_KEY. A model given web_search=on there can look
a fact up while it resolves a slot. See README.md.
`

// languageList renders the registered languages as prose: "C, C# and Go".
func languageList() string {
	supported := lang.Supported()
	names := make([]string, len(supported))
	for i, l := range supported {
		names[i] = l.Name
	}
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n\n%s", err, usage)
		return 2
	}
	if opts.help {
		fmt.Print(usage)
		return 0
	}
	if opts.version {
		fmt.Println(versionString())
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.update {
		return runUpdate(ctx)
	}
	if opts.config {
		return runConfig()
	}
	if opts.usage {
		return runUsage(os.Stdout)
	}

	image, err := imageref.Load(opts.image)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	prompt, err := loadPrompt(opts.prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	cfg, err := pslrc.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	// The log is one history per user, so it lives in the home directory rather
	// than wherever psl happened to be run. A log that cannot be opened is
	// worth a warning, not a failed compile.
	var logger *psllog.Logger
	if home, err := os.UserHomeDir(); err != nil {
		fmt.Fprintf(os.Stderr, "psl: warning: %v\n", err)
	} else if logger, err = psllog.Open(home); err != nil {
		fmt.Fprintf(os.Stderr, "psl: warning: %v\n", err)
	}

	compileOpts := compiler.Options{
		Path:    opts.path,
		Config:  cfg,
		Image:   image,
		Prompt:  prompt,
		Log:     logger,
		Version: strings.TrimSpace(versionFile),
	}
	if opts.resolve {
		compileOpts.Path = "runtime.txt.psl"
		result, err := compiler.CompileSource(ctx, opts.instruction, compileOpts)
		if errors.Is(err, compiler.ErrNoSlots) {
			fmt.Fprintln(os.Stderr, "psl: resolve needs one complete :: instruction :: slot")
			return 2
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "psl: %v\n", err)
			return 1
		}
		if result.Remaining != 0 {
			fmt.Fprintln(os.Stderr, "psl: resolve accepts exactly one AI slot")
			return 2
		}
		fmt.Fprint(os.Stdout, result.Source)
		return 0
	}
	if opts.run {
		code, err := runCompiled(ctx, compileOpts, opts.programArgs, os.Stdin, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "psl: %v\n", err)
			return 1
		}
		return code
	}

	result, err := compiler.Compile(ctx, compileOpts)
	if errors.Is(err, compiler.ErrNoSlots) {
		fmt.Fprintf(os.Stderr, "psl: %s has no AI slots left\n", opts.path)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	printResolved(os.Stderr, opts.path, result)
	if result.Remaining > 0 {
		fmt.Fprintf(os.Stderr, "psl: %d slot(s) remaining, run psl again\n", result.Remaining)
	} else {
		fmt.Fprintf(os.Stderr, "psl: no slots remaining\n")
	}
	return 0
}

// runUpdate replaces this executable with the newest published release.
func runUpdate(ctx context.Context) int {
	result, err := updater.Update(ctx, updater.Options{
		Current: strings.TrimSpace(versionFile),
		Log:     os.Stderr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	if !result.Updated {
		fmt.Fprintf(os.Stderr, "psl: already on the latest release (%s)\n", result.Latest)
		return 0
	}
	fmt.Fprintf(os.Stderr, "psl: updated %s from %s to %s\n", result.Path, result.Previous, result.Latest)
	if result.URL != "" {
		fmt.Fprintf(os.Stderr, "psl: %s\n", result.URL)
	}
	return 0
}

// runConfig opens the .pslrc in the user's editor: the one psl would read here,
// or a new one in the home directory when there is none. The file is parsed
// again afterwards, so a typo is reported now rather than on the next compile.
func runConfig() int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	path := pslrc.EditPath(dir)
	created, err := pslrc.Create(path, exampleFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	if created {
		fmt.Fprintf(os.Stderr, "psl: created %s\n", path)
	}
	if err := editor.Open(path); err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	if _, err := pslrc.Parse(string(data), path); err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "psl: %s\n", path)
	return 0
}

// runUsage totals what has been spent per model, from the log psl appends to
// as it compiles. The table goes to stdout so it can be piped into something
// else; which log it was read from goes to stderr, with everything else psl
// says about its own workings.
func runUsage(out io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	report, err := psllog.Summarize(home)
	if err != nil && !errors.Is(err, psllog.ErrNoLog) {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}
	if report.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "psl: warning: %s: %d unreadable line(s) skipped\n", report.Path, report.Skipped)
	}
	// No log at all and a log holding nothing readable come to the same thing:
	// there is nothing to report yet, which is not a failure.
	if len(report.Models) == 0 {
		fmt.Fprintf(os.Stderr, "psl: no requests logged yet (%s)\n", report.Path)
		return 0
	}
	printReport(out, report)
	fmt.Fprintf(os.Stderr, "psl: %s%s\n", report.Path, period(report.Total))
	return 0
}

// printReport writes the per-model table, a column per number and a row per
// model, padded so the numbers line up under their headings.
func printReport(out io.Writer, report psllog.Report) {
	// The errors column is only there when something failed: a column of
	// zeroes would be noise in the ordinary case.
	withErrors := report.Total.Errors > 0

	row := func(t psllog.Totals) []string {
		cells := []string{t.Model, strconv.Itoa(t.Requests)}
		if withErrors {
			cells = append(cells, strconv.Itoa(t.Errors))
		}
		return append(cells, strconv.Itoa(t.InputTokens), strconv.Itoa(t.OutputTokens), strconv.Itoa(t.TotalTokens))
	}

	header := []string{"MODEL", "REQUESTS"}
	if withErrors {
		header = append(header, "ERRORS")
	}
	header = append(header, "INPUT", "OUTPUT", "TOTAL")

	rows := [][]string{header}
	for _, model := range report.Models {
		rows = append(rows, row(model))
	}
	// A single model's row is already the whole log, so the total is only
	// worth adding when there is more than one row to add up.
	if len(report.Models) > 1 {
		total := report.Total
		total.Model = "TOTAL"
		rows = append(rows, row(total))
	}

	widths := make([]int, len(header))
	for _, cells := range rows {
		for i, cell := range cells {
			widths[i] = max(widths[i], len([]rune(cell)))
		}
	}
	for _, cells := range rows {
		// The model is a name and reads from the left; the numbers are compared
		// down the column and so line up on the right.
		fmt.Fprintf(out, "%-*s", widths[0], cells[0])
		for i, cell := range cells[1:] {
			fmt.Fprintf(out, "  %*s", widths[i+1], cell)
		}
		fmt.Fprintln(out)
	}
}

// period is the span the report covers, as a parenthesised suffix — empty when
// no entry carried a time, and one date when they all fall on the same day.
func period(total psllog.Totals) string {
	if total.First.IsZero() || total.Last.IsZero() {
		return ""
	}
	const day = "2006-01-02"
	first, last := total.First.Format(day), total.Last.Format(day)
	if first == last {
		return fmt.Sprintf(" (%s)", first)
	}
	return fmt.Sprintf(" (%s to %s)", first, last)
}

type options struct {
	path        string
	image       string
	prompt      string
	programArgs []string
	run         bool
	resolve     bool
	instruction string
	config      bool
	usage       bool
	update      bool
	help        bool
	version     bool
}

// parseArgs accepts flags before or after the file argument, since the README
// writes the file first.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--" && opts.run:
			if opts.path == "" {
				return opts, errors.New("run needs an input file before --")
			}
			opts.programArgs = append(opts.programArgs, args[i+1:]...)
			i = len(args)
		case arg == "-h" || arg == "--help":
			opts.help = true
			return opts, nil
		case arg == "-v" || arg == "--version":
			opts.version = true
			return opts, nil
		case arg == "-i" || arg == "--image":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			opts.image = args[i]
		case strings.HasPrefix(arg, "--image="):
			opts.image = strings.TrimPrefix(arg, "--image=")
		case arg == "-p" || arg == "--prompt":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			i++
			opts.prompt = args[i]
		case strings.HasPrefix(arg, "--prompt="):
			opts.prompt = strings.TrimPrefix(arg, "--prompt=")
		case strings.HasPrefix(arg, "-") && arg != "-":
			return opts, fmt.Errorf("unknown option %q", arg)
		// These are commands only as the first argument, so a file really named
		// update or config is still compilable as ./update.
		case arg == "update" && i == 0:
			opts.update = true
		case arg == "config" && i == 0:
			opts.config = true
		case arg == "usage" && i == 0:
			opts.usage = true
		case arg == "run" && i == 0:
			opts.run = true
		case arg == "resolve" && i == 0:
			opts.resolve = true
		default:
			if opts.update {
				return opts, fmt.Errorf("update takes no arguments, got %q", arg)
			}
			if opts.config {
				return opts, fmt.Errorf("config takes no arguments, got %q", arg)
			}
			if opts.usage {
				return opts, fmt.Errorf("usage takes no arguments, got %q", arg)
			}
			if opts.resolve {
				if opts.instruction != "" {
					return opts, fmt.Errorf("resolve accepts one AI slot, got %q and %q", opts.instruction, arg)
				}
				opts.instruction = arg
				continue
			}
			if opts.path != "" {
				return opts, fmt.Errorf("psl compiles one file per run, got %q and %q", opts.path, arg)
			}
			opts.path = arg
		}
	}
	if opts.run && opts.path == "" {
		return opts, errors.New("run needs an input file")
	}
	if opts.resolve && opts.instruction == "" {
		return opts, errors.New("resolve needs one AI slot")
	}
	if opts.path == "" && !opts.update && !opts.config && !opts.usage && !opts.resolve {
		return opts, errors.New("no input file")
	}
	return opts, nil
}

// loadPrompt interprets the --prompt argument: the guidance itself, or the
// contents of the file it names. A file is worth accepting because a briefing
// on the API being written against outlives any one slot, and psl resolves one
// slot per run — the same text would otherwise be retyped on every run. Text
// describing how to fill a file in is not plausibly also the name of a file
// sitting next to it, so an existing file wins without an explicit flag.
func loadPrompt(arg string) (string, error) {
	if arg == "" {
		return "", nil
	}
	if info, err := os.Stat(arg); err != nil || !info.Mode().IsRegular() {
		return arg, nil
	}
	data, err := os.ReadFile(arg)
	if err != nil {
		return "", fmt.Errorf("read prompt %s: %w", arg, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("prompt file %s is empty", arg)
	}
	return string(data), nil
}

// tokens renders what the request cost, for endpoints that report it.
func tokens(u llm.Usage) string {
	if u.TotalTokens == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d tokens: %d in, %d out)", u.TotalTokens, u.InputTokens, u.OutputTokens)
}

func printResolved(out io.Writer, path string, result *compiler.Result) {
	fmt.Fprintf(out, "psl: %s resolved with %s%s — %s\n",
		path, result.Model, tokens(result.Usage), summarize(result.Instruction))
}

func summarize(instruction string) string {
	instruction = strings.Join(strings.Fields(instruction), " ")
	const max = 72
	runes := []rune(instruction)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return instruction
}

func versionString() string {
	released := strings.TrimSpace(versionFile)
	if released == "" {
		released = "unknown"
	}
	build := strings.TrimSpace(version)
	if build == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			build = info.Main.Version
		}
	}
	if build == "" || build == released || build == "v"+released {
		return "psl " + released
	}
	return fmt.Sprintf("psl %s (%s)", released, build)
}
