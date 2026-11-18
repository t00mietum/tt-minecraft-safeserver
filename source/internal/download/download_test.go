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
