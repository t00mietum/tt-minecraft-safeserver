// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A name or chat line can't send escape codes or flip the text direction of
// what the operator reads.
func TestClean(t *testing.T) {
	defer testid.Line(t, "df78h")
	for _, c := range []struct{ in, want string }{
		{"Alex", "Alex"},
		{"Mal\x1b[2Jory\x7f\u0085", "Mal[2Jory"},
		{"evil\u202egpj.exe", "evilgpj.exe"},
		{"\u202a\u202b\u202c\u202d\u2066\u2067\u2068\u2069x", "x"},
		// Right-to-left text itself stays, as do characters next to the range.
		{"\u05e9\u05dc\u05d5\u05dd \u2029\u2065\u206a", "\u05e9\u05dc\u05d5\u05dd \u2029\u2065\u206a"},
	} {
		if got := Clean(c.in); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// watchOpens reports whether file was opened since the last call. inotify
// sees every open but an O_PATH one.
func watchOpens(t *testing.T, file string) func() bool {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if _, err := syscall.InotifyAddWatch(fd, file, syscall.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	return func() bool {
		buf := make([]byte, 4096)
		seen := false
		for {
			n, err := syscall.Read(fd, buf)
			if err != nil || n <= 0 {
				return seen
			}
			seen = true
		}
	}
}

// OpenRegular refuses anything but a regular file before it opens it, so a
// link to a device never runs the driver. A pipe stands in for the device,
// since inotify shows whether it was opened.
func TestOpenRegularChecksFirst(t *testing.T) {
	defer testid.Line(t, "df79h")
	dir := t.TempDir()
	fifo, plain := filepath.Join(dir, "fifo"), filepath.Join(dir, "plain")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	odd := []string{fifo, dir}
	// A socket's path has to fit in sun_path.
	if sock := filepath.Join(dir, "sock"); len(sock) < 100 {
		l, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		odd = append(odd, sock)
	}
	for _, target := range append(odd, plain) {
		if err := os.Symlink(target, target+".link"); err != nil {
			t.Fatal(err)
		}
	}
	opened := watchOpens(t, fifo)
	for _, file := range odd {
		for _, name := range []string{file, file + ".link"} {
			f, err := OpenRegular(name)
			if err == nil {
				_ = f.Close()
			}
			if !errors.Is(err, ErrNotRegular) {
				t.Errorf("%s: %v, want ErrNotRegular", name, err)
			}
		}
	}
	if opened() {
		t.Error("the pipe was opened before its kind was checked")
	}
	for _, name := range []string{plain, plain + ".link"} {
		f, err := OpenRegular(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil || string(got) != "text" {
			t.Errorf("%s read %q, %v", name, got, err)
		}
	}
	if _, err := OpenRegular(filepath.Join(dir, "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}

	// The watch itself sees an open.
	f, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !opened() {
		t.Error("inotify missed an open of the pipe")
	}
}
