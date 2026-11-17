// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A link that works on the host but goes through a path the container
// doesn't have stops start before anything changes hands. One between the
// mounted trees is fine, and so is a serverDir given as a link.
func TestStartBoxLinks(t *testing.T) {
	defer testid.Line(t, "df79c")
	g := newRig(t)
	srv := filepath.Dir(g.dir)
	v2 := filepath.Join(srv, "worlds", "v2")
	current := filepath.Join(srv, "worlds", "current")
	if err := os.MkdirAll(filepath.Join(v2, "world"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := func(target, name string) {
		t.Helper()
		_ = os.Remove(name)
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	link("v2", current)
	g.d.Cfg.OwnerTrees = []string{g.dir, current}
	world := filepath.Join(g.dir, "world")

	link(filepath.Join(current, "world"), world)
	err := g.d.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), world+" needs to point at "+filepath.Join(v2, "world")) || len(g.changes()) != 1 {
		t.Errorf("through current: err %v, ran %q", err, g.changes())
	}
	link("../worlds/current/world", world)
	if _, err := g.d.runArgs(); err == nil {
		t.Error("relative, through current: no error")
	}

	link(filepath.Join(v2, "world"), world)
	link("../server/plugins", filepath.Join(v2, "plugins"))
	link("gone", filepath.Join(g.dir, "dangles"))
	if _, err := g.d.runArgs(); err != nil {
		t.Errorf("between the trees: %v", err)
	}

	alias := filepath.Join(srv, "alias")
	link("server", alias)
	if err := os.Remove(world); err != nil {
		t.Fatal(err)
	}
	g.d.Cfg.ServerDir, g.d.Cfg.OwnerTrees = alias, []string{alias}
	args, err := g.d.runArgs()
	if i := slices.Index(args, "--workdir"); err != nil || i < 0 || args[i+1] != g.dir {
		t.Errorf("linked serverDir: %v, args %q", err, args)
	}
	link("server/plugins", filepath.Join(srv, "w"))
	link(filepath.Join(srv, "w"), filepath.Join(g.dir, "w"))
	if _, err := g.d.runArgs(); err == nil || !strings.Contains(err.Error(), filepath.Join(g.dir, "w")+" needs") {
		t.Errorf("link through a path outside: %v", err)
	}
}
