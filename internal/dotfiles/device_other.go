//go:build !unix

package dotfiles

import "io/fs"

func SameDevice(a, b fs.FileInfo) (bool, error) {
	return true, nil
}
