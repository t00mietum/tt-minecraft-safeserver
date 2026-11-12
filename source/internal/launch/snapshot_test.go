// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

var snapNow = func() time.Time { return time.Date(2026, 11, 12, 11, 0, 0, 0, time.Local) }

// snapRig is a rig with zfsDataset set, whose zfs snapshot fails when told.
func snapRig(t *testing.T, failSnap bool) *nativeRig {
	g := newNativeRig(t)
	g.d.Cfg.ZfsDataset = "tank/mc"
	g.d.Now, g.n.Now = snapNow, snapNow
	next := g.r.Reply
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		if failSnap && c.Args[0] == "zfs" && c.Args[1] == "snapshot" {
			return nil, errors.New("out of space")
		}
		return next(c)
	}
	return g
}

// index is where the first command starting with prefix ran, or -1.
func index(lines []string, prefix string) int {
	return slices.IndexFunc(lines, func(s string) bool { return strings.HasPrefix(s, prefix) })
}

// start snapshots right before the trees change hands, in either mode, and
// a failed snapshot stops it there. stop snapshots once the server is down,
// and the trees go back even when that fails.
func TestSnapshots(t *testing.T) {
	defer testid.Line(t, "df77q")
	ctx := context.Background()
	pre := "zfs snapshot -- tank/mc@20261112-110000_pre_tt-minecraftd-start"
	post := "zfs snapshot -- tank/mc@20261112-110000_post_tt-minecraftd-stop"
	for _, native := range []bool{false, true} {
		g := snapRig(t, false)
		g.d.DryRun, g.n.DryRun = true, true
		start := g.d.Start
		if native {
			start = g.n.Start
		}
		if err := start(ctx, StartMode{}); err != nil {
			t.Fatal(err)
		}
		lines := g.changes()
		at := index(lines, pre)
		if at < 0 || at+1 >= len(lines) || !strings.HasPrefix(lines[at+1], "sudo chown -R -- minecraftd:") || index(g.r.Lines(), "zfs list") < 0 {
			t.Errorf("native %v: ran %q", native, g.r.Lines())
		}

		g = snapRig(t, true)
		start = g.d.Start
		if native {
			start = g.n.Start
		}
		err := start(ctx, StartMode{})
		if err == nil || !strings.Contains(err.Error(), "can't snapshot tank/mc") || index(g.r.Lines(), "sudo chown") >= 0 {
			t.Errorf("native %v, snapshot fails: err %v, ran %q", native, err, g.r.Lines())
		}
	}

	g := snapRig(t, false)
	g.states = []string{"running", "running", "exited"}
	if err := g.d.Stop(ctx, 0); err != nil {
		t.Fatal(err)
	}
	lines := g.changes()
	at := index(lines, post)
	if at < 0 || at+1 >= len(lines) || !strings.HasPrefix(lines[at+1], "sudo chown -R -- alice:") || !slices.Equal(g.console.sent, []string{"stop"}) {
		t.Errorf("stop ran %q", g.r.Lines())
	}

	g = snapRig(t, true)
	g.states = []string{"running", "running", "exited"}
	err := g.d.Stop(ctx, 0)
	if err == nil || !strings.Contains(err.Error(), "can't snapshot") || index(g.r.Lines(), "sudo chown -R -- alice:") < 0 {
		t.Errorf("stop, snapshot fails: err %v, ran %q", err, g.r.Lines())
	}

	// Not running, as after a crash: the trees go back, with no snapshot.
	g = snapRig(t, false)
	g.states = []string{"exited"}
	if err := g.d.Stop(ctx, 0); err != nil || index(g.r.Lines(), "zfs") >= 0 {
		t.Errorf("not running: err %v, ran %q", err, g.r.Lines())
	}

	// No dataset, no zfs.
	g = newNativeRig(t)
	g.states = []string{"running", "running", "exited"}
	if err := g.d.Stop(ctx, 0); err != nil || index(g.r.Lines(), "zfs") >= 0 {
		t.Errorf("no dataset: err %v, ran %q", err, g.r.Lines())
	}
}
