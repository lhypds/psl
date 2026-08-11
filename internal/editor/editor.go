// Package editor opens a file in the terminal editor the user has chosen,
// $VISUAL or $EDITOR, falling back to whichever of the usual ones is installed.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Environment variables naming the editor, in the order they are consulted.
// $VISUAL comes first by long convention: $EDITOR may be a line editor kept for
// programs that only need one, while $VISUAL is the full-screen one.
const (
	EnvVisual = "VISUAL"
	EnvEditor = "EDITOR"
)

// fallbacks are tried in order when neither variable is set. They are the
// editors a machine is likely to have without anyone having chosen one.
func fallbacks() []string {
	if runtime.GOOS == "windows" {
		return []string{"notepad"}
	}
	return []string{"vim", "vi", "nano"}
}

// LookPath returns the editor command and any arguments it was configured with,
// so that VISUAL="emacs -nw" or EDITOR="code --wait" works as written. The value
// is split on spaces, which is all a command line in an environment variable can
// portably carry: an editor whose path contains a space needs a wrapper script.
func LookPath() ([]string, error) {
	for _, env := range []string{EnvVisual, EnvEditor} {
		if command := strings.Fields(os.Getenv(env)); len(command) > 0 {
			if _, err := exec.LookPath(command[0]); err != nil {
				return nil, fmt.Errorf("$%s is %q, which is not installed: %w", env, command[0], err)
			}
			return command, nil
		}
	}
	for _, name := range fallbacks() {
		if _, err := exec.LookPath(name); err == nil {
			return []string{name}, nil
		}
	}
	return nil, fmt.Errorf("no editor found: set $%s to the one you use, for example %s=vim",
		EnvEditor, EnvEditor)
}

// Open runs the editor on path and waits for it to exit. The editor inherits the
// terminal, since a full-screen one has nowhere else to draw.
func Open(path string) error {
	command, err := LookPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(command[0], append(command[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", strings.Join(command, " "), path, err)
	}
	return nil
}
