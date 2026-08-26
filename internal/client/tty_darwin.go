//go:build darwin

package client

import (
	"os"
	"syscall"
	"unsafe"
)

// fileIsTerminal reports whether f is a real tty by attempting the BSD
// read-termios ioctl (TIOCGETA), which fails against /dev/null, pipes, and
// regular files.
func fileIsTerminal(f *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
