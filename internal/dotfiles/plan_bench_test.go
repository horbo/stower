package dotfiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/horbo/stower/internal/config"
)

func benchMkdir(b *testing.B, path string) {
	b.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		b.Fatal(err)
	}
}

func benchWrite(b *testing.B, path string) {
	b.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		b.Fatal(err)
	}
}

func buildBenchDeepChainTree(b *testing.B, root string, depth, breadth int) {
	b.Helper()
	dir := root
	for level := 0; level < depth; level++ {
		benchMkdir(b, dir)
		for i := 0; i < breadth-1; i++ {
			benchWrite(b, filepath.Join(dir, fmt.Sprintf("file-%02d-%02d", level, i)))
		}
		dir = filepath.Join(dir, fmt.Sprintf("sub-%02d", level))
	}
	benchMkdir(b, dir)
	benchWrite(b, filepath.Join(dir, "leaf"))
}

func BenchmarkBuildAdoptPlanDeep(b *testing.B) {
	root := b.TempDir()
	target := filepath.Join(root, "target")
	dotfiles := filepath.Join(root, "dotfiles")
	benchMkdir(b, target)
	benchMkdir(b, dotfiles)
	deep := filepath.Join(target, "deep")
	buildBenchDeepChainTree(b, deep, 6, 8)
	nested := filepath.Join(deep, "sub-00", "nested")
	benchMkdir(b, filepath.Join(nested, ".git"))
	benchWrite(b, filepath.Join(nested, "file"))
	paths := config.Paths{Target: target, Dotfiles: dotfiles}
	staging := Staging{deep: "deep"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildAdoptPlanContext(context.Background(), paths, staging)
	}
}
