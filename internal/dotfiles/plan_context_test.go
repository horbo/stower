package dotfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type cancelOnCheckContext struct {
	context.Context
	stopAt int
	checks int
}

func (c *cancelOnCheckContext) Err() error {
	c.checks++
	if c.checks >= c.stopAt {
		return context.Canceled
	}
	return nil
}

func TestBuildAdoptPlanContextPropagatesCancellationFromFinalPackage(t *testing.T) {
	paths := newPaths(t)
	first := filepath.Join(paths.Target, ".first")
	last := filepath.Join(paths.Target, ".last")
	writeFile(t, first, "x")
	writeFile(t, filepath.Join(last, "child", "file"), "x")
	ctx := &cancelOnCheckContext{Context: context.Background(), stopAt: 8}

	plan := BuildAdoptPlanContext(ctx, paths, Staging{first: "a", last: "z"})
	if !errors.Is(plan.Fatal, context.Canceled) {
		t.Fatalf("BuildAdoptPlanContext fatal = %v, want context.Canceled", plan.Fatal)
	}
}

func TestBuildAdoptPlanContextHonorsCancellation(t *testing.T) {
	paths := newPaths(t)
	file := filepath.Join(paths.Target, ".zshrc")
	writeFile(t, file, "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := BuildAdoptPlanContext(ctx, paths, Staging{file: "zsh"})
	if !errors.Is(plan.Fatal, context.Canceled) {
		t.Fatalf("BuildAdoptPlanContext fatal = %v, want context.Canceled", plan.Fatal)
	}
}

func TestBuildAdoptPlanContextBlocksNestedGitScanError(t *testing.T) {
	paths := newPaths(t)
	root := filepath.Join(paths.Target, ".private")
	blocked := filepath.Join(root, "blocked")
	writeFile(t, filepath.Join(blocked, "file"), "x")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("the test process can read mode-000 directories")
	}

	plan := BuildAdoptPlanContext(context.Background(), paths, Staging{root: "private"})
	if len(plan.Packages) != 1 || len(plan.Packages[0].Blocked) != 1 {
		t.Fatalf("plan = %+v, want one blocked entry", plan)
	}
	if len(plan.Packages[0].Moves) != 0 {
		t.Fatalf("moves = %+v, want none", plan.Packages[0].Moves)
	}
}
