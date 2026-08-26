//go:build !linux && !darwin

package client

import "os"

// fileIsTerminal falls back to the character-device heuristic on platforms
// without a termios ioctl. This can report true for /dev/null, so on these
// platforms the picker may prompt in redirected-from-device contexts; Linux
// and macOS use the precise termios probe.
func fileIsTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
