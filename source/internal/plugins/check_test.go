// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestCheckJars(t *testing.T) {
	defer testid.Line(t, "df5ye00")
	dir := t.TempDir()
	put := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(text string) string {
		s := sha256.Sum256([]byte(text))
		return hex.EncodeToString(s[:])
	}
	put("plugins/a.jar", "a")
	put("plugins/b.jar", "b")
	put("plugins/notes.txt", "not a jar")
	approved := []Approved{
		{Name: "plugins/a.jar", SHA256: hash("a")},
		{Name: "plugins/b.jar", SHA256: hash("old b")},
		{Name: "plugins/gone.jar", SHA256: hash("gone")},
		{Name: "paper-26.3-244.jar", SHA256: hash("paper")},
	}
	var sums Sums
	c, err := CheckJars(dir, approved, &sums)
	want := Check{Jars: 2, Changed: []string{"plugins/b.jar"}, Missing: []string{"plugins/gone.jar"}}
	if err != nil || !reflect.DeepEqual(c, want) || c.OK() {
		t.Errorf("CheckJars = %+v, %v; want %+v", c, err, want)
	}

	// A jar swapped for one of another size is hashed again, cache or not.
	put("plugins/a.jar", "evil a")
	put("plugins/c.jar", "c")
	c, err = CheckJars(dir, approved, &sums)
	if err != nil || !reflect.DeepEqual(c.Changed, []string{"plugins/a.jar", "plugins/b.jar"}) || !reflect.DeepEqual(c.New, []string{"plugins/c.jar"}) {
		t.Errorf("after the swap: %+v, %v", c, err)
	}

	// Same size, new time: hashed again too.
	put("plugins/a.jar", "a")
	approved[1].SHA256 = hash("b")
	if c, err := CheckJars(dir, approved[:2], &sums); err != nil || len(c.Changed) != 0 || !reflect.DeepEqual(c.New, []string{"plugins/c.jar"}) {
		t.Errorf("after the fix: %+v, %v", c, err)
	}
	put("plugins/a.jar", "z")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "plugins/a.jar"), old, old); err != nil {
		t.Fatal(err)
	}
	if c, _ := CheckJars(dir, approved[:2], &sums); !reflect.DeepEqual(c.Changed, []string{"plugins/a.jar"}) {
		t.Errorf("same size swap missed: %+v", c)
	}
}

// A jar in plugins/ that links to a pipe, a socket, or a device counts as
// changed, and isn't opened: opening a device runs its driver, as root in
// repair, setup, update and a boot start. Neither is a pinned folder that
// links to one. A pipe stands in for the device, since inotify shows whether
// it was opened.
func TestJarLinksNotOpened(t *testing.T) {
	defer testid.Line(t, "df79j00")
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(outside, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{"plugins/pipe.jar": fifo}
	// A socket's path has to fit in sun_path.
	if sock := filepath.Join(outside, "sock"); len(sock) < 100 {
		l, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		links["plugins/sock.jar"] = sock
	}
	// The extensions folder as a link to the pipe isn't opened either.
	if err := os.MkdirAll(filepath.Join(dir, "plugins", "Geyser-Spigot"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fifo, filepath.Join(dir, "plugins", "Geyser-Spigot", "extensions")); err != nil {
		t.Fatal(err)
	}
	approved := make([]Approved, 0, len(links))
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		approved = append(approved, Approved{Name: name, SHA256: "0"})
	}
	ino, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(ino)
	if _, err := syscall.InotifyAddWatch(ino, fifo, syscall.IN_OPEN); err != nil {
		t.Fatal(err)
	}
	opened := func() bool {
		n, err := syscall.Read(ino, make([]byte, 4096))
		return err == nil && n > 0
	}

	var sums Sums
	if c, err := CheckJars(dir, approved, &sums); err != nil || c.Jars != len(links) || len(c.Changed) != len(links) || len(c.New) != 0 {
		t.Errorf("CheckJars = %+v, %v; want all %d changed", c, err, len(links))
	}
	for name := range links {
		file := filepath.Join(dir, name)
		if _, err := sums.Sum(file); !errors.Is(err, ErrNotRegular) {
			t.Errorf("Sum(%s): %v", name, err)
		}
		if _, err := Describe(Jar{name, file}); !errors.Is(err, ErrNotRegular) {
			t.Errorf("Describe(%s): %v", name, err)
		}
	}
	if opened() {
		t.Error("the pipe was opened before its kind was checked")
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

// Glob characters in serverDir are part of its name, not a pattern. Read as a
// pattern, mc[1] matches only mc1, so no jar was found and the check passed.
func TestJarsGlobInServerDir(t *testing.T) {
	defer testid.Line(t, "df79p00")
	for _, name := range []string{"mc[1]", "mc*", "mc?", `mc\x`, "[mc"} {
		dir := filepath.Join(t.TempDir(), name)
		ext := filepath.Join(dir, "plugins", "Geyser-Spigot", "extensions")
		if err := os.MkdirAll(ext, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"plugins/evil.jar", "plugins/Geyser-Spigot/extensions/x.jar", "paper-1.jar"} {
			if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink("paper-1.jar", filepath.Join(dir, "paper.jar")); err != nil {
			t.Fatal(err)
		}
		want := []string{"plugins/evil.jar", "plugins/Geyser-Spigot/extensions/x.jar", "paper-1.jar"}
		c, err := CheckJars(dir, nil, &Sums{})
		if err != nil || c.OK() || c.Jars != len(want) || !reflect.DeepEqual(c.New, want) {
			t.Errorf("%s: CheckJars = %+v, %v; want new %q", name, c, err, want)
		}
	}
}

// The folder paper.jar points to is the server account's to name, so the
// refusal of one outside the server folder shows it cleaned.
func TestJarsOutsideCleaned(t *testing.T) {
	defer testid.Line(t, "df79w00")
	dir, outside := t.TempDir(), filepath.Join(t.TempDir(), "x\x1b]0;owned\x07")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "paper-26.3-249.jar"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "paper-26.3-249.jar"), filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	_, err := Jars(dir)
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "x]0;owned/paper-26.3-249.jar, outside") {
		t.Errorf("%v", err)
	}
}
