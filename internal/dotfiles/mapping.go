package dotfiles

import (
	"path/filepath"
	"strings"
)

const DotPrefix = "dot-"

func TargetToPackage(pkg, relTarget string) string {
	rel := cleanRel(relTarget)
	if rel == "" {
		return pkg
	}
	first, rest := splitFirst(rel)
	if len(first) > 1 && strings.HasPrefix(first, ".") && first != ".." {
		first = DotPrefix + first[1:]
	}
	return filepath.Join(pkg, first, rest)
}

func PackageToTarget(relPkg string) string {
	rel := cleanRel(relPkg)
	if rel == "" {
		return ""
	}
	first, rest := splitFirst(rel)
	if len(first) > len(DotPrefix) && strings.HasPrefix(first, DotPrefix) {
		first = "." + first[len(DotPrefix):]
	}
	return filepath.Join(first, rest)
}

func PackageOf(relDotfiles string) string {
	rel := cleanRel(relDotfiles)
	if rel == "" {
		return ""
	}
	first, _ := splitFirst(rel)
	if first == "." || first == ".." {
		return ""
	}
	return first
}

func cleanRel(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return ""
	}
	return strings.TrimPrefix(cleaned, string(filepath.Separator))
}

func splitFirst(rel string) (string, string) {
	if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
		return rel[:i], rel[i+1:]
	}
	return rel, ""
}
