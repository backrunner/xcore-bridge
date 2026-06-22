package main

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
)

var version = "dev"

func versionCommand(args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return errors.New("version accepts at most one flag")
	}
	if len(args) == 1 {
		switch args[0] {
		case "--verbose", "-v":
			fmt.Fprintf(stdout, "xcore-bridge %s\n", version)
			fmt.Fprintf(stdout, "xray-core %s\n", xrayCoreVersion())
			return nil
		default:
			return fmt.Errorf("unknown version flag %q", args[0])
		}
	}
	fmt.Fprintln(stdout, version)
	return nil
}

func xrayCoreVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/xtls/xray-core" {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			if dep.Version != "" {
				return dep.Version
			}
			return "(devel)"
		}
	}
	return "unknown"
}
