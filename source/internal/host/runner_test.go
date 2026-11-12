// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package host

import (
	"bytes"
	"context"
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
