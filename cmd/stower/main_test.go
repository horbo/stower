package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusAndRestowCommands(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "dotfiles")
	if err := os.MkdirAll(filepath.Join(repo, "misc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "misc", "dot-bar"), []byte("repo"), 0644); err != nil {
		t.Fatal(err)
	}
	flags := []string{"--target", root, "--dotfiles", repo}
	var stdout, stderr bytes.Buffer
	status := func() error { stdout.Reset(); return run(append([]string{"status"}, flags...), &stdout, &stderr) }
	if err := status(); !errors.Is(err, errIssues) || !strings.Contains(stdout.String(), "missing") {
		t.Fatalf("%s err=%v", stdout.String(), err)
	}
	stdout.Reset()
	if err := run(append([]string{"restow"}, flags...), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "LINK: .bar") {
		t.Fatal("restow output missing: " + stdout.String())
	}
	if err := status(); err != nil || !strings.Contains(stdout.String(), "ok") {
		t.Fatalf("%s err=%v", stdout.String(), err)
	}
	if err := os.Remove(filepath.Join(root, ".bar")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".bar"), []byte("target"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := status(); !errors.Is(err, errIssues) || !strings.Contains(stdout.String(), "replaced") {
		t.Fatalf("%s err=%v", stdout.String(), err)
	}
	if err := run(append([]string{"restow"}, flags...), &stdout, &stderr); err == nil {
		t.Fatal("restow conflict returned success")
	}
	data, err := os.ReadFile(filepath.Join(root, ".bar"))
	if err != nil || string(data) != "target" {
		t.Fatal("restow modified conflicting target")
	}
}

func TestStatusNeedsNoStowAndRejectsUnknownArguments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	args := []string{"status", "--target", root, "--dotfiles", filepath.Join(root, "dotfiles")}
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "PACKAGE") {
		t.Fatal(stdout.String())
	}
	if err := run([]string{"unknown"}, &stdout, &stderr); err == nil {
		t.Fatal("unknown subcommand accepted")
	}
}
