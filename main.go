// Command psl compiles a Prompt Script Language file: it resolves the first
// remaining `:: ... ::` slot with an AI model and writes the result back into
// the file in place of the slot. Run it repeatedly until no slots remain.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"psl/internal/compiler"
	"psl/internal/editor"
	"psl/internal/imageref"
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

const usage = `psl — Prompt Script Language compiler

Usage:
  psl <file.psl> [--image <base64_image>] [--prompt <text>]
  psl config
  psl update

Each run resolves exactly one AI slot: psl finds the first remaining
:: instruction :: in the file, generates its output, and writes the result
back over the slot. Run psl again for the next slot.

Commands:
  config               edit .pslrc in $VISUAL, $EDITOR, or vim
  update               replace this executable with the latest GitHub release

Options:
  -i, --image <data>   image given to the slot resolved on this run;
                       accepts a file path, a data: URL, or raw base64
  -p, --prompt <text>  guidance added to the system prompt: the API the code
                       has to fit, what each parameter means, in what units;
                       accepts the text itself or a path to a text file
  -h, --help           show this help
  -v, --version        show the version

Configuration lives in .pslrc, looked up in the current directory and then in
your home directory. It is optional: without it psl uses OPENAI_API_KEY if that
is set, otherwise ANTHROPIC_API_KEY. See README.md.
`

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

	result, err := compiler.Compile(ctx, compiler.Options{
		Path:    opts.path,
		Config:  cfg,
		Image:   image,
		Prompt:  prompt,
		Log:     logger,
		Version: strings.TrimSpace(versionFile),
	})
	if errors.Is(err, compiler.ErrNoSlots) {
		fmt.Fprintf(os.Stderr, "psl: %s has no AI slots left\n", opts.path)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "psl: %s resolved with %s%s — %s\n",
		opts.path, result.Model, tokens(result.Usage), summarize(result.Instruction))
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

type options struct {
	path    string
	image   string
	prompt  string
	config  bool
	update  bool
	help    bool
	version bool
}

// parseArgs accepts flags before or after the file argument, since the README
// writes the file first.
func parseArgs(args []string) (options, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
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
		default:
			if opts.update {
				return opts, fmt.Errorf("update takes no arguments, got %q", arg)
			}
			if opts.config {
				return opts, fmt.Errorf("config takes no arguments, got %q", arg)
			}
			if opts.path != "" {
				return opts, fmt.Errorf("psl compiles one file per run, got %q and %q", opts.path, arg)
			}
			opts.path = arg
		}
	}
	if opts.path == "" && !opts.update && !opts.config {
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
