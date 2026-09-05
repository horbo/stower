package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/tui"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "stower: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("stower", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dotfiles := flags.String("dotfiles", "", "dotfiles repository directory (default ~/dotfiles, or $STOWER_DOTFILES)")
	target := flags.String("target", "", "directory stow links into (default $HOME)")
	showVersion := flags.Bool("version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "stower %s\n", version)
		return nil
	}

	paths, err := config.Resolve(*dotfiles, *target, os.Getenv)
	if err != nil {
		return err
	}
	_, stowVersion, err := config.StowBinary()
	if err != nil {
		return err
	}

	program := tea.NewProgram(tui.New(paths, stowVersion), tea.WithOutput(stdout))
	_, err = program.Run()
	return err
}
