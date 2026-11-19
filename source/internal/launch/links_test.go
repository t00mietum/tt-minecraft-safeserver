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
	defer testid.Line(t, "1a3js9g")
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

// A link out of the trees is called that, not given its own target as the
// fix. A folder start can't read is a warning, and doesn't stop the start
// (backlog 2026111715275408).
func TestBoxLinksOutsideAndUnread(t *testing.T) {
	defer testid.Line(t, "1a3jz84")
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	g := newRig(t)
	world := filepath.Join(g.dir, "world")
	if err := os.Symlink("/etc/hostname", world); err != nil {
		t.Fatal(err)
	}
	_, err := g.d.runArgs()
	if err == nil || !strings.Contains(err.Error(), world+" leads to /etc/hostname, which isn't in any tree") || strings.Contains(err.Error(), "needs to point at") {
		t.Errorf("out of the trees: %v", err)
	}
	if err := os.Remove(world); err != nil {
		t.Fatal(err)
	}

	hidden := filepath.Join(g.dir, "hidden")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hostname", filepath.Join(hidden, "world")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hidden, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hidden, 0o700) })
	g.out.Reset()
	if _, err := g.d.runArgs(); err != nil {
		t.Errorf("unreadable folder stopped the start: %v", err)
	}
	if out := g.out.String(); !strings.Contains(out, "warning: ") || !strings.Contains(out, "can't be read") || !strings.Contains(out, hidden) {
		t.Errorf("unreadable folder: no warning in %q", out)
	}
}
