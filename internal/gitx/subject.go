package gitx

import (
	"fmt"
	"strings"
)

type Operation string

const (
	Add    Operation = "add"
	Remove Operation = "remove"
	Update Operation = "update"
	Fix    Operation = "fix"
	Restow Operation = "restow"
)

type Change struct {
	Operation Operation
	Packages  []string
	Entry     string
	Files     int
}

const subjectPrefix = "stower: "

func Subject(change Change) string {
	switch change.Operation {
	case Restow:
		return subjectPrefix + "restow"
	case Fix:
		if entry := oneLine(change.Entry); entry != "" {
			return subjectPrefix + "fix " + entry
		}
		return subjectPrefix + "fix"
	case Remove:
		if entry := oneLine(change.Entry); entry != "" {
			return subjectPrefix + "remove " + entry
		}
	}
	operation := change.Operation
	if oneLine(string(operation)) == "" {
		operation = Update
	}
	verb := oneLine(string(operation))
	names := make([]string, 0, len(change.Packages))
	for _, pkg := range change.Packages {
		if name := oneLine(pkg); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return subjectPrefix + verb
	}
	subject := subjectPrefix + verb + " " + strings.Join(names, ", ")
	if change.Files > 0 && (operation == Add || operation == Update) {
		subject += fmt.Sprintf(" (%s)", files(change.Files))
	}
	return subject
}

func files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func oneLine(s string) string {
	replaced := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(replaced), " "))
}
