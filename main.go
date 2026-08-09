// Command psl compiles a Prompt Script Language file: it resolves the first
// remaining `:: ... ::` slot with an AI model and writes the result back into
// the file in place of the slot. Run it repeatedly until no slots remain.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"psl/internal/compiler"
	"psl/internal/imageref"
	"psl/internal/pslrc"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = ""

const usage = `psl — Prompt Script Language compiler

Usage:
  psl <file.psl> [--image <base64_image>]

Each run resolves exactly one AI slot: psl finds the first remaining
:: instruction :: in the file, generates its output, and writes the result
back over the slot. Run psl again for the next slot.

Options:
  -i, --image <data>   image given to the slot resolved on this run;
                       accepts a file path, a data: URL, or raw base64
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

	image, err := imageref.Load(opts.image)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := compiler.Compile(ctx, compiler.Options{
		Path:   opts.path,
		Config: cfg,
		Image:  image,
	})
	if errors.Is(err, compiler.ErrNoSlots) {
		fmt.Fprintf(os.Stderr, "psl: %s has no AI slots left\n", opts.path)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "psl: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "psl: %s resolved with %s — %s\n",
		opts.path, result.Model, summarize(result.Instruction))
	if result.Remaining > 0 {
		fmt.Fprintf(os.Stderr, "psl: %d slot(s) remaining, run psl again\n", result.Remaining)
	} else {
		fmt.Fprintf(os.Stderr, "psl: no slots remaining\n")
	}
	return 0
}

type options struct {
	path    string
	image   string
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
		case strings.HasPrefix(arg, "-") && arg != "-":
			return opts, fmt.Errorf("unknown option %q", arg)
		default:
			if opts.path != "" {
				return opts, fmt.Errorf("psl compiles one file per run, got %q and %q", opts.path, arg)
			}
			opts.path = arg
		}
	}
	if opts.path == "" {
		return opts, errors.New("no input file")
	}
	return opts, nil
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
	if version != "" {
		return "psl " + version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return "psl " + info.Main.Version
	}
	return "psl (devel)"
}
