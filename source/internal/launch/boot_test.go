// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// With the boot unit enabled, start runs the checks and then systemctl start,
// in both modes, and a refused check never reaches systemctl.
func TestStartThroughUnit(t *testing.T) {
	defer testid.Line(t, "df78s")
	g := newRig(t)
	g.d.Boot = true
	if err := g.d.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"sudo -v", "sudo systemctl start tt-minecraftd.service"}
	if got := g.changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("container: ran %q, want %q", got, want)
	}

	g = newRig(t)
	n := &Native{Kit: g.d.Kit}
	n.Boot = true
	if err := n.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	if got := g.changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("native: ran %q, want %q", got, want)
	}

	g = newRig(t)
	g.d.Boot = true
	if err := os.WriteFile(filepath.Join(g.dir, "plugins", "new.jar"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := g.d.Start(context.Background(), StartMode{}); err == nil || strings.Contains(strings.Join(g.changes(), "\n"), "systemctl") {
		t.Errorf("unapproved jar: %v, ran %q", err, g.changes())
	}
}

// With the boot unit enabled, stop counts down, runs systemctl stop, whose
// ExecStop saves the worlds, and hands the trees back. A server the unit
// didn't start still gets the stop command.
func TestStopThroughUnit(t *testing.T) {
	defer testid.Line(t, "df78t")
	for _, unitStops := range []bool{true, false} {
		g := newRig(t)
		g.d.Boot = true
		g.states = []string{"running"}
		stopped := false
		g.r.Reply = func(c host.Cmd) ([]byte, error) {
			if c.Args[0] == "systemctl" {
				stopped = unitStops
				return nil, nil
			}
			if stopped && len(c.Args) > 2 && c.Args[1] == "ps" {
				return []byte("exited\n"), nil
			}
			return g.reply(c)
		}
		if unitStops {
			// The unit's stop already waited.
			g.d.Cfg.StopTimeout = 0
		}
		if err := g.d.Stop(context.Background(), 2); err != nil && unitStops {
			t.Fatal(err)
		}
		sent := []string{"say The server stops in 2 seconds.", "say The server stops in 1 second."}
		changes := []string{"sudo -v", "sudo systemctl stop tt-minecraftd.service"}
		if unitStops {
			changes = append(changes, "sudo chown -R -- alice:minecraftd "+host.Quote([]string{g.dir}))
		} else {
			sent = append(sent, "stop")
		}
		if !reflect.DeepEqual(g.console.sent, sent) {
			t.Errorf("unit stops it %v: sent %q, want %q", unitStops, g.console.sent, sent)
		}
		if got := g.changes(); !reflect.DeepEqual(got[:min(len(got), len(changes))], changes) {
			t.Errorf("unit stops it %v: ran %q, want %q first", unitStops, got, changes)
		}
	}
}
