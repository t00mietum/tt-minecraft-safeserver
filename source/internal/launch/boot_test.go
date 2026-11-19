// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// With the boot unit enabled, start runs the checks and then systemctl start,
// in both modes, and a refused check never reaches systemctl.
func TestStartThroughUnit(t *testing.T) {
	defer testid.Line(t, "1a3jqe4")
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
	// Was an unapproved jar until 2026111914334569 took approval out.
	props := filepath.Join(g.dir, "server.properties")
	text, err := os.ReadFile(props)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(props, []byte(strings.Replace(string(text), "white-list=true", "white-list=false", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := g.d.Start(context.Background(), StartMode{}); err == nil || strings.Contains(strings.Join(g.changes(), "\n"), "systemctl") {
		t.Errorf("whitelist off: %v, ran %q", err, g.changes())
	}
}

// With the boot unit enabled, stop counts down, runs systemctl stop, whose
// ExecStop saves the worlds, and hands the trees back. A server the unit
// didn't start still gets the stop command.
func TestStopThroughUnit(t *testing.T) {
	defer testid.Line(t, "1a3jqh8")
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

// Inside the boot unit, start --foreground leaves the hand-back to ExecStop's
// stop, which systemd runs after every start, and a wait that systemd's
// SIGTERM cuts short once the server is down is a clean stop. Outside it,
// both are as before.
func TestInsideUnitOneHandBack(t *testing.T) {
	defer testid.Line(t, "1a3jx6g")
	chowned := func(g *rig) bool {
		return strings.Contains(strings.Join(g.changes(), "\n"), "chown -R -- alice:minecraftd")
	}

	for _, inUnit := range []bool{true, false} {
		g := newRig(t)
		g.d.Root, g.d.InUnit = true, inUnit
		g.states = []string{"", "running"}
		g.log = doneLine
		if err := g.d.Start(context.Background(), StartMode{Foreground: true}); err != nil {
			t.Fatalf("container, in unit %v: %v", inUnit, err)
		}
		if chowned(g) == inUnit {
			t.Errorf("container, in unit %v: ran %q", inUnit, g.changes())
		}

		g = newRig(t)
		g.d.Root, g.d.InUnit = true, inUnit
		g.states = []string{"", "exited"}
		g.log = doneLine
		ctx, cancel := context.WithCancel(context.Background())
		g.r.Reply = func(c host.Cmd) ([]byte, error) {
			if len(c.Args) > 1 && c.Args[1] == "wait" {
				cancel()
				return nil, errors.New("docker wait: signal: terminated")
			}
			return g.reply(c)
		}
		err := g.d.Start(ctx, StartMode{Foreground: true})
		cancel()
		if (err == nil) != inUnit || chowned(g) {
			t.Errorf("container, SIGTERM in the wait, in unit %v: err %v, ran %q", inUnit, err, g.changes())
		}

		n := newNativeRig(t)
		n.n.Root, n.n.InUnit = true, inUnit
		n.states = []string{"inactive", "active", "active", "inactive"}
		n.launch = func() { n.writeLog(t, doneLine) }
		if err := n.n.Start(context.Background(), StartMode{Foreground: true}); err != nil {
			t.Fatalf("native, in unit %v: %v", inUnit, err)
		}
		if chowned(n.rig) == inUnit {
			t.Errorf("native, in unit %v: ran %q", inUnit, n.changes())
		}

		n = newNativeRig(t)
		n.n.Root, n.n.InUnit = true, inUnit
		n.states = []string{"inactive", "active", "inactive"}
		n.launch = func() { n.writeLog(t, doneLine) }
		ctx, cancel = context.WithCancel(context.Background())
		n.n.Sleep = func(time.Duration) { cancel() }
		err = n.n.Start(ctx, StartMode{Foreground: true})
		cancel()
		if (err == nil) != inUnit || chowned(n.rig) {
			t.Errorf("native, SIGTERM in the wait, in unit %v: err %v, ran %q", inUnit, err, n.changes())
		}
	}

	// A SIGTERM with the server still up is still a failure.
	g := newRig(t)
	g.d.Root, g.d.InUnit = true, true
	g.states = []string{"", "running"}
	g.log = doneLine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		if len(c.Args) > 1 && c.Args[1] == "wait" {
			cancel()
			return nil, errors.New("docker wait: signal: terminated")
		}
		return g.reply(c)
	}
	if err := g.d.Start(ctx, StartMode{Foreground: true}); err == nil {
		t.Error("SIGTERM with the server up: no error")
	}

	// ExecStop's stop still hands back, server up or not.
	for _, states := range [][]string{{"exited"}, {"running", "exited"}} {
		g := newRig(t)
		g.d.Root, g.d.InUnit = true, true
		g.states = states
		if err := g.d.Stop(context.Background(), 0); err != nil || !chowned(g) {
			t.Errorf("stop, states %q: err %v, ran %q", states, err, g.changes())
		}
	}
}
