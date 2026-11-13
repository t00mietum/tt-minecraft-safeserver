// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode"
)

// LogFile is latest.log. It's the same file in both modes, since the server
// tree is mounted in the container.
func LogFile(serverDir string) string {
	return filepath.Join(serverDir, "logs", "latest.log")
}

// OpenRegular opens a file in the server tree for reading. The server owns
// the tree, so it could swap a file for a pipe or a device, to hang a reader
// or feed it without end.
func OpenRegular(file string) (*os.File, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0) // #nosec G304 -- under the configured serverDir
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("%s isn't a regular file", file)
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

// MainThread is the thread console commands run on, so their answers are
// logged from it.
const MainThread = "Server thread"

// logHead is the start of a latest.log line, as in
// "[12:00:03] [Server thread/INFO]: ". A thread name can hold a slash.
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
