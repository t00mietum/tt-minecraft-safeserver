// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// LogFile is latest.log. It's the same file in both modes, since the server
// tree is mounted in the container.
func LogFile(serverDir string) string {
	return filepath.Join(serverDir, "logs", "latest.log")
}

// ErrNotRegular is a file in the server tree that's a pipe, a device, a
// socket, or a folder.
var ErrNotRegular = errors.New("not a regular file")

// OpenRegular opens a file in the server tree for reading, after links. The
// server owns the tree, so it could swap a file for a pipe, or a link to a
// device, to hang a reader or feed it without end. Opening a device runs its
// driver, and some act on the open alone, such as a watchdog that reboots
// the host when it's closed without its stop byte, so the kind is checked
// before the file is opened to read.
func OpenRegular(file string) (*os.File, error) {
	// O_PATH gets the inode without opening it: no driver runs and a pipe
	// doesn't wait. Reopening through /proc/self/fd opens that same inode,
	// so a link swapped after the check can't change what's read.
	pfd, err := syscall.Open(file, oPath|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: file, Err: err}
	}
	defer syscall.Close(pfd)
	var want syscall.Stat_t
	if err := syscall.Fstat(pfd, &want); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: file, Err: err}
	}
	if want.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, fmt.Errorf("%s: %w", file, ErrNotRegular)
	}
	// O_NONBLOCK, in case the reopen isn't the same file after all.
	fd, err := syscall.Open("/proc/self/fd/"+strconv.Itoa(pfd), syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: file, Err: err}
	}
	f := os.NewFile(uintptr(fd), file)
	var got syscall.Stat_t
	err = syscall.Fstat(fd, &got)
	if err == nil && (got.Dev != want.Dev || got.Ino != want.Ino || got.Mode&syscall.S_IFMT != syscall.S_IFREG) {
		err = fmt.Errorf("%s changed while it was opened: %w", file, ErrNotRegular)
	}
	if err != nil {
		_ = f.Close() // only read from, so nothing is lost
		return nil, err
	}
	return f, nil
}

// Clean drops control characters and the ones that flip text direction, so a
// player's name or chat line can't send escape codes to the operator's
// terminal or change how the rest of the line reads.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, s)
}

// CleanLines is Clean for text of several lines, such as a log tail or an
// error, and keeps the line breaks.
func CleanLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = Clean(l)
	}
	return strings.Join(lines, "\n")
}

// Cut shortens s to limit bytes on a character boundary, and marks the cut.
func Cut(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// MainThread is the thread console commands run on, so their answers are
// logged from it.
const MainThread = "Server thread"

// logHead is the start of a latest.log line, as in
// "[12:00:03] [Server thread/INFO]: ". A thread name can have a slash in it.
var logHead = regexp.MustCompile(`^\[[0-9:]+\] \[(.+)/(TRACE|DEBUG|INFO|WARN|ERROR|FATAL)\]: `)

// LogThread gives the thread that logged a latest.log line. ok is false for a
// line without one, such as the second line of a message.
func LogThread(line string) (thread string, ok bool) {
	m := logHead.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}
