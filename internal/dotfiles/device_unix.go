//go:build unix

package dotfiles

import (
	"fmt"
	"io/fs"
	"syscall"
)

func SameDevice(a, b fs.FileInfo) (bool, error) {
	statA, ok := a.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("cannot read the device of %s", a.Name())
	}
	statB, ok := b.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("cannot read the device of %s", b.Name())
	}
	return statA.Dev == statB.Dev, nil
}
