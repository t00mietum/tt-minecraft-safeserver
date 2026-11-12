// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// script writes a shell script into a temp folder and gives its path.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil { // #nosec G306 -- the test runs it
		t.Fatal(err)
	}
	return path
}

func TestCommandAsks(t *testing.T) {
	defer testid.Line(t, "df762")
	side := t.TempDir()
	sh := script(t, `cat > "$1/stdin"
pwd > "$1/pwd"
ls -A > "$1/ls"
echo "the answer"
echo "thinking..." >&2
`)
	got, err := Command{Argv: []string{sh, side}}.Ask(context.Background(), "the question\n")
	if err != nil || got != "the answer\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(side, name)) // #nosec G304 -- the test's own temp folder
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if read("stdin") != "the question\n" {
		t.Errorf("stdin %q", read("stdin"))
	}
	dir := strings.TrimSpace(read("pwd"))
	if !strings.Contains(filepath.Base(dir), "tt-minecraftd-agent-") || read("ls") != "" {
		t.Errorf("ran in %s, holding %q", dir, read("ls"))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s is still there: %v", dir, err)
	}
}

func TestCommandFailures(t *testing.T) {
	defer testid.Line(t, "df763")
	cases := map[string]struct {
		body    string
		timeout time.Duration
		says    string
	}{
		"exit":      {body: "echo 'not logged in' >&2\nexit 3\n", says: "exit status 3: not logged in"},
		"too long":  {body: "head -c 1100000 /dev/zero\n", says: "is over 1048576 bytes"},
		"too slow":  {body: "sleep 30 &\nwait\n", timeout: 300 * time.Millisecond, says: "didn't answer within 300ms"},
		"not there": {says: "no such file"},
	}
	for name, tc := range cases {
		argv := []string{filepath.Join(t.TempDir(), "missing")}
		if tc.body != "" {
			argv = []string{script(t, tc.body)}
		}
		start := time.Now()
		_, err := Command{Argv: argv, Timeout: tc.timeout}.Ask(context.Background(), "q")
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: got %v", name, err)
		}
		// The background sleep is killed with its parent, rather than
		// holding the output open until it ends.
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("%s: took %s", name, took)
		}
	}
}

func TestCommandNotAsRoot(t *testing.T) {
	defer testid.Line(t, "df764")
	old := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = old })
	side := t.TempDir()
	sh := script(t, `touch "$1/ran"`)
	if _, err := (Command{Argv: []string{sh, side}}).Ask(context.Background(), "q"); err == nil ||
		!strings.Contains(err.Error(), "doesn't run as root") {
		t.Errorf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(side, "ran")); !os.IsNotExist(err) {
		t.Errorf("the command ran: %v", err)
	}
}
