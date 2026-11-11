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
