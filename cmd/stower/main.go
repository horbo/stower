package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"

	"github.com/horbo/stower/internal/config"
	"github.com/horbo/stower/internal/doctor"
	"github.com/horbo/stower/internal/dotfiles"
	"github.com/horbo/stower/internal/stow"
	"github.com/horbo/stower/internal/tui"
)

var version = "dev"
var errIssues = errors.New("link issues found")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errIssues) {
			fmt.Fprintf(os.Stderr, "stower: %v\n", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	command := ""
	if len(args) > 0 && (args[0] == "status" || args[0] == "restow") {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("stower", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dotfilesDir := flags.String("dotfiles", "", "dotfiles repository directory (default ~/dotfiles, or $STOWER_DOTFILES)")
	target := flags.String("target", "", "directory stow links into (default $HOME)")
	noMouse := flags.Bool("no-mouse", false, "disable mouse reporting so the terminal keeps its own text selection (also $STOWER_NO_MOUSE)")
	showVersion := flags.Bool("version", false, "print the version and exit")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if flags.NArg() > 0 {
		if command == "" && flags.NArg() == 1 && (flags.Arg(0) == "status" || flags.Arg(0) == "restow") {
			command = flags.Arg(0)
		} else {
			return fmt.Errorf("unexpected arguments: %v", flags.Args())
		}
	}
	if *showVersion {
		fmt.Fprintf(stdout, "stower %s\n", version)
		return nil
	}

	paths, err := config.Resolve(*dotfilesDir, *target, os.Getenv)
	if err != nil {
		return err
	}
	if command == "status" {
		return printStatus(paths, stdout)
	}
	bin, stowVersion, err := config.StowBinary()
	if err != nil {
		return err
	}

	if command == "restow" {
		packages, err := dotfiles.ListPackages(paths)
		if err != nil {
			return err
		}
		runner := stow.Runner{Bin: bin, Dotfiles: paths.Dotfiles, Target: paths.Target}
		var failures []error
		for _, pkg := range packages {
			result := runner.RestowStream(stdout, pkg)
			if result.Err != nil {
				failures = append(failures, result.Err)
			}
		}
		return errors.Join(failures...)
	}
	model := tui.New(paths, stowVersion).WithMouse(config.MouseEnabled(*noMouse, os.Getenv))
	program := tea.NewProgram(model, tea.WithOutput(stdout))
	_, err = program.Run()
	return err
}

func printStatus(paths config.Paths, stdout io.Writer) error {
	issues, reports, inspectErr := doctor.Inspect(paths)
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "PACKAGE\tENTRY\tTARGET\tSTATE"); err != nil {
		return err
	}
	for _, report := range reports {
		for _, entry := range report.Entries {
			if _, err := fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", report.Package, entry.Entry.PkgRel, entry.Entry.TargetPath(paths), entry.State); err != nil {
				return err
			}
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if inspectErr != nil {
		return inspectErr
	}
	if len(issues) > 0 {
		return errIssues
	}
	return nil
}
