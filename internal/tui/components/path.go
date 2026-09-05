package components

import (
	"path/filepath"
	"strings"
)

func DisplayPath(path, home string) string {
	if path == "" || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := strings.TrimSuffix(home, string(filepath.Separator)) + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(filepath.Separator) + path[len(prefix):]
	}
	return path
}
