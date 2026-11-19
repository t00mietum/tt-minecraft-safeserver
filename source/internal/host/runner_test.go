// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package host

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestQuote(t *testing.T) {
	defer testid.Line(t, "df5xc")
	got := Quote([]string{"chown", "-R", "--", "minecraftd:minecraftd", "/srv/a b", "it's", ""})
	want := `chown -R -- minecraftd:minecraftd '/srv/a b' 'it'\''s' ''`
	if got != want {
		t.Errorf("Quote = %s, want %s", got, want)
	}
}

// Under dry run a change prints and doesn't run, with sudo marked, while a
// command that only looks still runs.
func TestExecDryRun(t *testing.T) {
	defer testid.Line(t, "df5xd")
	var out bytes.Buffer
	e := &Exec{DryRun: true, Out: &out, Err: &out}
	ctx := context.Background()
	if _, err := e.Run(ctx, Cmd{Args: []string{"false"}, Sudo: true}); err != nil {
		t.Fatalf("dry run ran it: %v", err)
	}
	got, err := e.Run(ctx, Cmd{Args: []string{"echo", "hi"}, Look: true})
	if err != nil || string(got) != "hi\n" {
		t.Errorf("look = %q, %v; want hi", got, err)
	}
	e.Root = true
	if _, err := e.Run(ctx, Cmd{Args: []string{"chmod", "-R", "o-rwx", "/srv/x"}, Sudo: true}); err != nil {
		t.Fatal(err)
	}
	want := "would run: sudo false\nwould run: chmod -R o-rwx /srv/x\n"
	if out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

func TestExecError(t *testing.T) {
	defer testid.Line(t, "df5xe")
	var errOut bytes.Buffer
	e := &Exec{Out: &errOut, Err: &errOut}
	_, err := e.Run(context.Background(), Cmd{Args: []string{"sh", "-c", "echo oops >&2; exit 3"}})
	if err == nil || !strings.Contains(err.Error(), "sh -c 'echo oops >&2; exit 3'") {
		t.Errorf("err = %v, want it to name the command", err)
	}
	if errOut.String() != "oops\n" {
		t.Errorf("stderr = %q, want it passed on", errOut.String())
	}
	if _, err := e.Run(context.Background(), Cmd{}); err == nil {
		t.Error("empty command ran")
	}
}

// A Tty command's stderr still goes to the terminal, and to ErrCopy too, for
// console to tell docker attach's detach from sudo's or docker's own exit 1.
func TestExecTtyErrCopy(t *testing.T) {
	defer testid.Line(t, "df7b0")
	term, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	keep := os.Stderr
	os.Stderr = term
	defer func() { os.Stderr = keep }()
	var copied bytes.Buffer
	e := &Exec{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	_, err = e.Run(context.Background(), Cmd{Args: []string{"sh", "-c", "echo read escape sequence >&2; exit 1"}, Tty: true, ErrCopy: &copied})
	os.Stderr = keep
	shown, _ := os.ReadFile(term.Name())
	if err == nil || copied.String() != "read escape sequence\n" || string(shown) != "read escape sequence\n" {
		t.Errorf("err %v, copied %q, shown %q", err, copied.String(), shown)
	}
}

// A command run as the service account goes through sudo -u, or runuser
// when the tool is already root, since root may have no sudo.
func TestExecAs(t *testing.T) {
	defer testid.Line(t, "df5y0")
	var out bytes.Buffer
	e := &Exec{DryRun: true, Out: &out}
	ctx := context.Background()
	c := Cmd{Args: []string{"tmux", "send-keys", "-l", "--", "say hi"}, As: "minecraftd", Sudo: true}
	if _, err := e.Run(ctx, c); err != nil {
		t.Fatal(err)
	}
	e.Root = true
	if _, err := e.Run(ctx, c); err != nil {
		t.Fatal(err)
	}
	want := "would run: sudo -u minecraftd -- tmux send-keys -l -- 'say hi'\nwould run: runuser -u minecraftd -- tmux send-keys -l -- 'say hi'\n"
	if out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
	f := &Fake{}
	_, _ = f.Run(ctx, c) // a Fake with no Reply can't fail
	if got := f.Lines()[0]; got != "sudo -u minecraftd -- tmux send-keys -l -- 'say hi'" {
		t.Errorf("Lines = %q", got)
	}
}

// QuietErr keeps stderr off the terminal and puts it in the error.
func TestExecQuietErr(t *testing.T) {
	defer testid.Line(t, "df5yf")
	var errOut bytes.Buffer
	e := &Exec{Err: &errOut, QuietErr: true}
	_, err := e.Run(context.Background(), Cmd{Look: true, Args: []string{"sh", "-c", "echo oops >&2; echo more >&2; exit 3"}})
	if err == nil || !strings.HasSuffix(err.Error(), ": oops") || errOut.Len() != 0 {
		t.Errorf("err %v, stderr %q", err, errOut.String())
	}
	e.QuietErr = false
	if _, err := e.Run(context.Background(), Cmd{Look: true, Args: []string{"sh", "-c", "echo oops >&2; exit 3"}}); err == nil || strings.HasSuffix(err.Error(), ": oops") || errOut.String() != "oops\n" {
		t.Errorf("without QuietErr: err %v, stderr %q", err, errOut.String())
	}
}

// Quiet does the same for one command, such as a try with a fallback.
func TestExecQuietCmd(t *testing.T) {
	defer testid.Line(t, "df77r")
	var errOut bytes.Buffer
	e := &Exec{Err: &errOut}
	_, err := e.Run(context.Background(), Cmd{Quiet: true, Look: true, Args: []string{"sh", "-c", "echo denied >&2; exit 1"}})
	if err == nil || !strings.HasSuffix(err.Error(), ": denied") || errOut.Len() != 0 {
		t.Errorf("err %v, stderr %q", err, errOut.String())
	}
}

func TestExecStdin(t *testing.T) {
	defer testid.Line(t, "df66j")
	e := &Exec{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	out, err := e.Run(context.Background(), Cmd{Args: []string{"cat"}, Stdin: []byte("table inet x\n")})
	if err != nil || string(out) != "table inet x\n" {
		t.Errorf("got %q, %v", out, err)
	}
	var printed strings.Builder
	e = &Exec{DryRun: true, Out: &printed}
	if _, err := e.Run(context.Background(), Cmd{Args: []string{"install", "-T", "/dev/stdin", "/etc/x"}, Stdin: []byte("y")}); err != nil || printed.String() != "would run: install -T /dev/stdin /etc/x\n" {
		t.Errorf("dry run printed %q, %v", printed.String(), err)
	}
}

// NoPrompt puts -n on the sudo the runner adds, so a dry run's look never
// asks for a password, and one sudo refuses says what it couldn't check.
func TestExecNoPrompt(t *testing.T) {
	defer testid.Line(t, "df7aw")
	dir := t.TempDir()
	log := filepath.Join(dir, "sudo.log")
	// Without SUDO_CACHED, -n refuses as a run-out timestamp does, and
	// anything else would ask.
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\n" +
		"if [ \"$1\" = -n ]; then [ -n \"$SUDO_CACHED\" ] || { echo 'sudo: a password is required' >&2; exit 1; }; shift; fi\n" +
		"[ \"$1\" = -u ] && shift 3\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUDO_CACHED", "")
	ctx := context.Background()
	var out, errOut bytes.Buffer
	e := &Exec{DryRun: true, NoPrompt: true, Out: &out, Err: &errOut}
	for _, c := range []Cmd{
		{Look: true, Sudo: true, Args: []string{"podman", "ps"}},
		{Look: true, As: "minecraftd", Args: []string{"ls"}},
	} {
		_, err := e.Run(ctx, c)
		if !errors.Is(err, ErrNoPassword) || !strings.Contains(err.Error(), "couldn't check sudo -n ") {
			t.Errorf("%v: err %v, want it to say what it couldn't check", c.Args, err)
		}
	}
	if errOut.Len() != 0 || out.Len() != 0 {
		t.Errorf("printed %q, stderr %q", out.String(), errOut.String())
	}

	t.Setenv("SUDO_CACHED", "1")
	got, err := e.Run(ctx, Cmd{Look: true, Sudo: true, Args: []string{"sh", "-c", "echo hi; echo warn >&2"}})
	if err != nil || string(got) != "hi\n" || errOut.String() != "warn\n" {
		t.Errorf("cached: got %q, %v, stderr %q", got, err, errOut.String())
	}
	errOut.Reset()
	_, err = e.Run(ctx, Cmd{Look: true, Sudo: true, Args: []string{"sh", "-c", "echo nope >&2; exit 3"}})
	if err == nil || errors.Is(err, ErrNoPassword) || errOut.String() != "nope\n" {
		t.Errorf("a failed look: %v, stderr %q", err, errOut.String())
	}

	e = &Exec{Out: &out, Err: &errOut}
	if _, err := e.Run(ctx, Cmd{Sudo: true, Args: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	logged, _ := os.ReadFile(log)
	want := "-n podman ps\n-n -u minecraftd -- ls\n-n sh -c echo hi; echo warn >&2\n-n sh -c echo nope >&2; exit 3\ntrue\n"
	if string(logged) != want {
		t.Errorf("sudo got\n%s\nwant\n%s", logged, want)
	}
	if !NewExec(true).NoPrompt || NewExec(false).NoPrompt {
		t.Error("NoPrompt doesn't follow dry run")
	}
}
