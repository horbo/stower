//go:build unix

package dotfiles

import (
	"io/fs"
	"syscall"
	"testing"
	"time"
)

type fakeInfo struct {
	name string
	sys  any
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return fs.ModeDir }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return true }
func (f fakeInfo) Sys() any           { return f.sys }

func TestSameDevice(t *testing.T) {
	first := fakeInfo{name: "target", sys: &syscall.Stat_t{Dev: 1}}
	sameDev := fakeInfo{name: "dotfiles", sys: &syscall.Stat_t{Dev: 1}}
	otherDev := fakeInfo{name: "dotfiles", sys: &syscall.Stat_t{Dev: 2}}

	same, err := SameDevice(first, sameDev)
	if err != nil || !same {
		t.Errorf("SameDevice on one device = (%v, %v), want (true, nil)", same, err)
	}

	same, err = SameDevice(first, otherDev)
	if err != nil || same {
		t.Errorf("SameDevice on two devices = (%v, %v), want (false, nil)", same, err)
	}

	if _, err := SameDevice(fakeInfo{name: "target"}, sameDev); err == nil {
		t.Error("SameDevice: want an error when the device cannot be read")
	}
}
