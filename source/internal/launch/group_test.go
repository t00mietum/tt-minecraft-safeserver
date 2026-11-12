// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A login that can't read the tree once it's the server's, as right after
// setup, gets told why: native start before it changes anything, and cmd
// before it sends the line. Container start, which reads the log through the
// runtime, warns. --debug and root don't read the tree.
func TestLoginGroup(t *testing.T) {
	defer testid.Line(t, "df68g")
	g := newNativeRig(t)
	inGroup(t, groupNextLogin)
	err := g.n.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "isn't in group minecraftd yet") || !strings.Contains(err.Error(), "log in again") || len(g.changes()) != 0 {
		t.Errorf("next login: err %v, ran %q", err, g.changes())
	}

	g = newNativeRig(t)
	inGroup(t, groupMissing)
	err = g.n.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "sudo gpasswd --add alice minecraftd") || len(g.changes()) != 0 {
		t.Errorf("not in it: err %v, ran %q", err, g.changes())
	}

	for _, root := range []bool{false, true} {
		g = newNativeRig(t)
		inGroup(t, groupNextLogin)
		g.n.Root = root
		g.states = []string{"inactive", "active"}
		mode := StartMode{Debug: true}
		if root {
			g.launch = func() { g.writeLog(t, doneLine) }
			mode = StartMode{}
		}
		if err := g.n.Start(context.Background(), mode); err != nil {
			t.Errorf("root %v: %v", root, err)
		}
	}

	c := newCmdRig(t, "")
	inGroup(t, groupNextLogin)
	if err := c.n.SendCmd(context.Background(), "list"); err == nil || !strings.Contains(err.Error(), "log in again") || len(c.console.sent) != 0 {
		t.Errorf("cmd: err %v, sent %q", err, c.console.sent)
	}

	d := newRig(t)
	inGroup(t, groupNextLogin)
	d.states = []string{"", "running"}
	d.log = doneLine
	if err := d.d.Start(context.Background(), StartMode{}); err != nil || !strings.Contains(d.out.String(), "warning: this login isn't in group minecraftd yet") {
		t.Errorf("container: err %v, output %q", err, d.out.String())
	}

	// An unknown group is left to the steps that need it.
	g = newNativeRig(t)
	inGroup(t, groupUnknown)
	g.states = []string{"inactive", "active"}
	g.launch = func() { g.writeLog(t, doneLine) }
	if err := g.n.Start(context.Background(), StartMode{}); err != nil {
		t.Errorf("unknown: %v", err)
	}
}

// The real lookup: this process's own group, and one that isn't there.
func TestLoginGroupHost(t *testing.T) {
	defer testid.Line(t, "df68h")
	g, err := user.LookupGroupId(strconv.Itoa(os.Getegid()))
	if err != nil {
		t.Skip("own group has no name:", err)
	}
	if state, _ := loginGroup(g.Name); state != groupHere {
		t.Errorf("own group %s: %d", g.Name, state)
	}
	if state, _ := loginGroup("no-such-group-tt"); state != groupUnknown {
		t.Errorf("missing group: %d", state)
	}
}
