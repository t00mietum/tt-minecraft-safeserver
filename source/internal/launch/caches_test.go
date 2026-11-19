// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// plantCaches puts code where Paper would load it from without a check, and
// files start must leave alone. libraries is a link to a folder of the world
// when linked is set, since container start refuses one out of the trees. It gives the paths that must be gone, and those that must
// stay.
func plantCaches(t *testing.T, dir string, linked bool) (gone, kept []string) {
	t.Helper()
	gone = []string{
		"plugins/.paper-remapped/index.json",
		"plugins/.paper-remapped/Geyser-Spigot.jar",
		"plugins/.paper-remapped/unknown-origin/x.jar",
	}
	kept = []string{"plugins/Geyser-Spigot.jar", "plugins/Geyser-Spigot/config.yml", "world/level.dat", "paper-26.3-244.jar"}
	if linked {
		outside := filepath.Join(dir, "world", "datapacks")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "keep.jar"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "libraries")); err != nil {
			t.Fatal(err)
		}
		gone = append(gone, "libraries")
		kept = append(kept, "world/datapacks/keep.jar")
	} else {
		gone = append(gone, "libraries/org/apache/commons/commons-text/1.12.0/commons-text-1.12.0.jar")
	}
	for _, f := range append(slices.Clone(gone), kept...) {
		path := filepath.Join(dir, f)
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("planted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return gone, kept
}

// runClear runs start's rm for real, as this test's own account stands in
// for the service account, and checks it came after the hand-off and before
// the launch.
func runClear(t *testing.T, g *rig, launch string) {
	t.Helper()
	changes := g.changes()
	at := -1
	for i, c := range g.r.Ran {
		if len(c.Args) > 0 && c.Args[0] == "rm" {
			if c.As != "minecraftd" || c.Sudo {
				t.Errorf("rm runs as %q, sudo %v; want the service account", c.As, c.Sudo)
			}
			if err := exec.Command(c.Args[0], c.Args[1:]...).Run(); err != nil {
				t.Fatal(err)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no rm in %q", changes)
	}
	handOff := slices.IndexFunc(changes, func(s string) bool { return strings.HasPrefix(s, "sudo chmod -R ug+rwX") })
	clear := slices.IndexFunc(changes, func(s string) bool { return strings.Contains(s, " rm -rf ") })
	start := slices.IndexFunc(changes, func(s string) bool { return strings.HasPrefix(s, launch) })
	if handOff < 0 || clear != handOff+1 || start <= clear {
		t.Errorf("order: hand-off %d, rm %d, launch %d in %q", handOff, clear, start, changes)
	}
}

// Code the server account leaves in Paper's library cache or remapped plugin
// folder doesn't outlast a restart, in either mode. A link there is removed,
// not followed.
func TestStartClearsCaches(t *testing.T) {
	defer testid.Line(t, "df7bg")
	for _, linked := range []bool{false, true} {
		for _, mode := range []string{"docker", "native"} {
			var g *rig
			var start func() error
			launch := "sudo docker create "
			if mode == "docker" {
				g = newRig(t)
				g.states = []string{"", "running"}
				g.log = doneLine
				start = func() error { return g.d.Start(context.Background(), StartMode{}) }
			} else {
				n := newNativeRig(t)
				n.states = []string{"inactive", "active"}
				n.launch = func() { n.writeLog(t, doneLine) }
				g, launch = n.rig, "sudo systemd-run "
				start = func() error { return n.n.Start(context.Background(), StartMode{}) }
			}
			gone, kept := plantCaches(t, g.dir, linked)
			if err := start(); err != nil {
				t.Fatalf("%s, linked %v: %v", mode, linked, err)
			}
			runClear(t, g, launch)
			for _, f := range gone {
				if _, err := os.Lstat(filepath.Join(g.dir, f)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s, linked %v: %s is still there (%v)", mode, linked, f, err)
				}
			}
			for _, f := range kept {
				if _, err := os.Stat(filepath.Join(g.dir, f)); err != nil {
					t.Errorf("%s, linked %v: %s is gone: %v", mode, linked, f, err)
				}
			}
		}
	}
}

// A cache start can't clear stops the start before the launch, and the trees
// go back.
func TestStartClearFails(t *testing.T) {
	defer testid.Line(t, "df7bh")
	g := newRig(t)
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		if c.Args[0] == "rm" {
			return nil, errors.New("rm: cannot remove: Permission denied")
		}
		return g.reply(c)
	}
	err := g.d.Start(context.Background(), StartMode{})
	got := g.changes()
	if err == nil || !strings.Contains(err.Error(), "clearing Paper's library cache") ||
		slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "docker create") }) ||
		!strings.Contains(got[len(got)-1], "chown -R -- alice:minecraftd") {
		t.Errorf("err %v, ran %q", err, got)
	}
}
