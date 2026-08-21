// Package cli implements the asshfs subcommands.
package cli

import (
	"fmt"
	"io"
	"os"
)

// Run dispatches a subcommand. args[0] is the program name; args[1] is the
// subcommand. Returns the process exit code.
func Run(args []string) int {
	return RunWith(args, os.Stdout, os.Stderr)
}

// RunWith is like Run but with explicit streams for testing.
func RunWith(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		usage(stderr)
		return 2
	}
	switch args[1] {
	case "reconcile":
		return cmdReconcile(stdout, stderr)
	case "install":
		return cmdInstall(stdout, stderr)
	case "uninstall":
		return cmdUninstall(stdout, stderr)
	case "list":
		return cmdList(stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %s\n", args[1])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: asshfs <reconcile|install|uninstall|list>")
}

func cmdReconcile(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "reconcile: not yet implemented")
	return 1
}

func cmdInstall(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "install: not yet implemented")
	return 1
}

func cmdUninstall(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "uninstall: not yet implemented")
	return 1
}

func cmdList(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "list: not yet implemented")
	return 1
}
