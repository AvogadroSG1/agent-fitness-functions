//go:build linux

package client

import (
	"os"
	"syscall"
	"unsafe"
)

// fileIsTerminal reports whether f is a real tty by attempting the Linux
// read-termios ioctl (TCGETS), which fails against /dev/null, pipes, and
// regular files.
func fileIsTerminal(f *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
