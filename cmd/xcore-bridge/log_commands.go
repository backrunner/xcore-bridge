package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/backrunner/xcore-bridge/internal/daemon"
)

func logCommand(args []string, stdout, stderr io.Writer) error {
	path, err := daemon.BridgeLogPath()
	if err != nil {
		return err
	}
	return printLogCommand("log", path, args, stdout)
}

func daemonLogCommand(args []string, stdout, stderr io.Writer) error {
	path, err := daemon.DaemonLogPath()
	if err != nil {
		return err
	}
	return printLogCommand("daemon log", path, args, stdout)
}

func printLogCommand(name, logPath string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lines := fs.Int("lines", 200, "number of recent log lines to show")
	follow := fs.Bool("follow", false, "follow appended log lines")
	pathOnly := fs.Bool("path", false, "print the log file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pathOnly {
		fmt.Fprintln(stdout, logPath)
		return nil
	}
	if *lines < 0 {
		return errors.New("--lines must be non-negative")
	}
	if err := printTail(stdout, logPath, *lines); err != nil {
		return err
	}
	if *follow {
		return followLog(stdout, logPath)
	}
	return nil
}

func printTail(w io.Writer, path string, lines int) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(w, "log file does not exist yet: %s\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if lines == 0 {
		return nil
	}
	var ring []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if len(ring) < lines {
			ring = append(ring, scanner.Text())
			continue
		}
		copy(ring, ring[1:])
		ring[len(ring)-1] = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, line := range ring {
		fmt.Fprintln(w, line)
	}
	return nil
}

func followLog(w io.Writer, path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		for {
			file, err = os.Open(path)
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if _, writeErr := io.WriteString(w, line); writeErr != nil {
				return writeErr
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		return err
	}
}
