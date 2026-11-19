// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package download

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A staged jar that the server swaps for a pipe is refused when opened to
// read its claims, rather than hanging the open.
func TestStagedOpenPipe(t *testing.T) {
	defer testid.Line(t, "df79n")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "PK not really a jar")
	}))
	defer ts.Close()
	dir := t.TempDir()
	dirfd, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(dirfd)
	s, err := Client{HTTP: ts.Client()}.Stage(context.Background(), dirfd, dir, "x.jar", ts.URL, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Drop()
	if f, err := s.Open(); err != nil {
		t.Errorf("regular file: %v", err)
	} else {
		_ = f.Close()
	}

	tmp := filepath.Join(dir, s.Temp)
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(tmp, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := s.Open()
		if err == nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a pipe was given back as the staged file")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Open hung on a pipe")
	}
}

// rawStatus answers 404 with a reason phrase holding escape codes.
func rawStatus(w http.ResponseWriter, _ *http.Request) {
	c, buf, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer c.Close()
	_, _ = buf.WriteString("HTTP/1.1 404 \x1b]0;owned\x07\x1b[2Jgone\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	_ = buf.Flush()
}

// A download server's reason phrase is its own text, so the error names the
// status by its code.
func TestStatusByCode(t *testing.T) {
	defer testid.Line(t, "df79x")
	ts := httptest.NewServer(http.HandlerFunc(rawStatus))
	t.Cleanup(ts.Close)
	_, err := Client{}.Get(context.Background(), ts.URL+"/x.jar")
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "/x.jar: 404 Not Found") {
		t.Errorf("%v", err)
	}
	if got := Status(599); got != "599" {
		t.Errorf("Status(599) = %q", got)
	}
}

// stageJar stages a small download in a new temp folder.
func stageJar(t *testing.T, body string) (string, *Staged) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	dir := t.TempDir()
	dirfd, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(dirfd) })
	s, err := Client{HTTP: ts.Client()}.Stage(context.Background(), dirfd, dir, "x.jar", ts.URL, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Drop)
	return dir, s
}

// The checked download stays 0600 while it waits for the operator, so the
// server account can't rewrite it, and gets 0660 only as it's kept.
func TestStagedPrivateUntilKept(t *testing.T) {
	defer testid.Line(t, "df7b3")
	dir, s := stageJar(t, "PK good jar")
	fi, err := os.Lstat(filepath.Join(dir, s.Temp))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("staged mode %o, want 600", got)
	}
	if err := s.Keep(); err != nil {
		t.Fatal(err)
	}
	fi, err = os.Lstat(filepath.Join(dir, "x.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o660 {
		t.Errorf("kept mode %o, want 660", got)
	}
}

// A file the server puts in place of the staged one isn't renamed over the
// good jar.
func TestStagedSwapRefused(t *testing.T) {
	defer testid.Line(t, "df7b4")
	dir, s := stageJar(t, "PK good jar")
	good := filepath.Join(dir, "x.jar")
	if err := os.WriteFile(good, []byte("PK old jar"), 0o660); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, s.Temp)
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("PK bad jar"), 0o660); err != nil {
		t.Fatal(err)
	}
	if f, err := s.Open(); err == nil {
		_ = f.Close()
		t.Error("Open gave back the swapped file")
	}
	if err := s.Keep(); err == nil {
		t.Error("Keep took the swapped file")
	}
	if b, _ := os.ReadFile(good); string(b) != "PK old jar" {
		t.Errorf("x.jar is now %q", b)
	}
}
