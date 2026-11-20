// Package cli implements the sphinx command.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// Exit codes. A run that both finds leaks and fails to read a file exits 1:
// the leak is the more urgent news.
const (
	ExitClean    = 0
	ExitFindings = 1
	ExitUsage    = 2
	ExitError    = 3
)

type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	IsTerminal     bool
}

var Version = ""

func version() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return "dev"
}

const usage = `Sphinx asks your files what they give away before you share them.

Usage:
  sphinx [gaze] [flags] FILE|DIR...   inspect files and report what they leak
  sphinx reveal [flags] FILE...       extract what the files hide
  sphinx seal [flags] FILE...         write copies with the leaks removed
  sphinx rules [RULE]                 explain each kind of leak
  sphinx version                      print the version

Use "-" as FILE to read standard input. Run "sphinx COMMAND -h" for flags.

Exit codes: 0 nothing at or above --fail-on, 1 leaks found,
2 usage error, 3 a file could not be read.
`

func Run(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return ExitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "gaze":
		return gaze(rest, env)
	case "reveal":
		return reveal(rest, env)
	case "seal":
		return seal(rest, env)
	case "rules":
		return rules(rest, env)
	case "version", "--version", "-v":
		fmt.Fprintln(env.Stdout, "sphinx", version())
		return ExitClean
	case "help", "-h", "-help", "--help":
		fmt.Fprint(env.Stdout, usage)
		return ExitClean
	}
	return gaze(args, env)
}

func newFlags(name, synopsis string, env Env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "Usage: sphinx %s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(interleaved(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitClean, false
		}
		return ExitUsage, false
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return ExitUsage, false
	}
	return 0, true
}

// interleaved moves flags after positional arguments to the front, so that
// "sphinx photo.jpg --format json" works like most command-line tools.
func interleaved(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if f := fs.Lookup(name); f != nil {
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		}
	}
	return append(append(flags, "--"), pos...)
}

func useColor(env Env, disabled bool) bool {
	if disabled || env.Getenv("NO_COLOR") != "" || env.Getenv("TERM") == "dumb" {
		return false
	}
	return env.IsTerminal || env.Getenv("FORCE_COLOR") != ""
}

func StdEnv() Env {
	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	return Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, IsTerminal: tty}
}
