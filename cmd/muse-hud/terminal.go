package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// isTTY reports whether w is a terminal (character device).
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

type winsize struct {
	rows, cols uint16
	xpix, ypix uint16
}

// makeRaw puts f's terminal into a raw-ish mode suitable for
// single-key reads (no echo, no canonical line buffering, no signal
// generation — Ctrl-C arrives as byte 3) and returns a restore
// function. Standard library only: TCGETS/TCSETS ioctls with
// syscall.Termios (Linux; muse-hud v1 is Linux-only per PLAN.md).
func makeRaw(f *os.File) (restore func(), err error) {
	fd := f.Fd()
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&old))); errno != 0 {
		return nil, errno
	}
	raw := old
	raw.Iflag &^= syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd,
		uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return nil, errno
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd,
			uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&old)))
	}, nil
}

// terminalWidth returns the column count of w's terminal via the
// TIOCGWINSZ ioctl, or 0 when w is not a terminal. Standard library
// only (Linux/macOS ioctl numbers via syscall.TIOCGWINSZ).
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 || ws.cols == 0 {
		return 0
	}
	return int(ws.cols)
}
