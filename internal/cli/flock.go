//go:build darwin

package cli

import (
	"os"
	"syscall"
)

func tryFlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func releaseFlock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
