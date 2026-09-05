package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kamilhorbowicz/stower/internal/config"
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

	if _, err := config.Resolve(*dotfiles, *target, os.Getenv); err != nil {
		return err
	}
	if _, _, err := config.StowBinary(); err != nil {
		return err
	}

	fmt.Fprintln(stderr, "stower: TUI not implemented yet")
	return nil
}
