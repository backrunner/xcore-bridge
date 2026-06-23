package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	if err := runWithIO(os.Args[1:], os.Stdout, os.Stderr, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "xcore-bridge:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	return runWithIO(args, stdout, stderr, nil)
}

func runWithIO(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout)
	case "xray-config":
		return xrayConfigCommand(args[1:], stdout)
	case "add":
		return addCommand(args[1:], stdout, stderr, stdin)
	case "remove":
		return removeCommand(args[1:], stdout, stderr)
	case "upgrade":
		return upgradeCommand(args[1:], stdout, stderr, stdin)
	case "version", "--version", "-v":
		return versionCommand(args[1:], stdout)
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
