// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

// Commented out by 2026111914334569: start no longer clears Paper's library cache, since library pinning went. 2026111914334585 moves the clearing to update.

// import (
// 	"archive/zip"
// 	"bytes"
// 	"context"
// 	"crypto/sha256"
// 	"encoding/hex"
// 	"errors"
// 	"os"
// 	"os/exec"
// 	"path/filepath"
// 	"slices"
// 	"strings"
// 	"testing"

// 	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
// 	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
// )

// // plantCaches puts code where Paper would load it from without a check, and
// // files start must leave alone, with an approved list to match. libraries is
// // a link to a folder of the world when linked is set. It gives the paths that
// // must be gone, and those that must stay.
// func plantCaches(t *testing.T, dir, list string, linked bool) (gone, kept []string) {
// 	t.Helper()
// 	sum := func(text string) string {
// 		s := sha256.Sum256([]byte(text))
// 		return hex.EncodeToString(s[:])
// 	}
// 	own := "libraries/com/google/guava/guava/33.3.1-jre/guava-33.3.1-jre.jar"
// 	var b bytes.Buffer
// 	z := zip.NewWriter(&b)
// 	w, err := z.Create("META-INF/libraries.list")
// 	if err == nil {
// 		_, err = w.Write([]byte(sum("planted") + "\tcom.google.guava:guava:33.3.1-jre\t" + strings.TrimPrefix(own, "libraries/") + "\n"))
// 	}
// 	if err == nil {
// 		err = z.Close()
// 	}
// 	if err == nil {
// 		err = os.WriteFile(filepath.Join(dir, "paper-26.3-244.jar"), b.Bytes(), 0o600)
// 	}
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	approvedLib := "libraries/org/a/a/1/a-1.jar"
// 	changedLib := "libraries/org/b/b/1/b-1.jar"
// 	text := "jar: paper-26.3-244.jar\n\tsha256: " + sum(b.String()) + "\njar: plugins/Geyser-Spigot.jar\n\tsha256: " + sum("") +
// 		"\njar: " + approvedLib + "\n\tsha256: " + sum("planted") + "\njar: " + changedLib + "\n\tsha256: " + sum("before") + "\n"
// 	if err := os.WriteFile(list, []byte(text), 0o600); err != nil {
// 		t.Fatal(err)
// 	}

// 	gone = []string{
// 		"plugins/.paper-remapped/Geyser-Spigot.jar",
// 		"plugins/.paper-remapped/unknown-origin/x.jar",
// 	}
// 	kept = []string{"plugins/.paper-remapped/index.json", "plugins/Geyser-Spigot/config.yml", "world/level.dat"}
// 	if linked {
// 		outside := filepath.Join(dir, "world", "datapacks")
// 		if err := os.MkdirAll(outside, 0o700); err != nil {
// 			t.Fatal(err)
// 		}
// 		if err := os.WriteFile(filepath.Join(outside, "keep.jar"), []byte("x"), 0o600); err != nil {
// 			t.Fatal(err)
// 		}
// 		if err := os.Symlink(outside, filepath.Join(dir, "libraries")); err != nil {
// 			t.Fatal(err)
// 		}
// 		gone = append(gone, "libraries")
// 		kept = append(kept, "world/datapacks/keep.jar")
// 	} else {
// 		gone = append(gone, "libraries/org/apache/commons/commons-text/1.12.0/commons-text-1.12.0.jar", changedLib)
// 		kept = append(kept, own, approvedLib, "libraries/org/apache/commons/commons-text/1.12.0/commons-text-1.12.0.pom")
// 	}
// 	for _, f := range append(slices.Clone(gone), kept...) {
// 		path := filepath.Join(dir, f)
// 		if _, err := os.Lstat(path); err == nil {
// 			continue
// 		}
// 		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
// 			t.Fatal(err)
// 		}
// 		if err := os.WriteFile(path, []byte("planted"), 0o600); err != nil {
// 			t.Fatal(err)
// 		}
// 	}
// 	return gone, kept
// }

// // runClear runs start's rm for real, as this test's own account stands in
// // for the service account, and checks it came after the hand-off and before
// // the launch.
// func runClear(t *testing.T, g *rig, launch string) {
// 	t.Helper()
// 	changes := g.changes()
// 	at := -1
// 	for i, c := range g.r.Ran {
// 		if len(c.Args) > 0 && c.Args[0] == "rm" {
// 			if c.As != "minecraftd" || c.Sudo {
// 				t.Errorf("rm runs as %q, sudo %v; want the service account", c.As, c.Sudo)
// 			}
// 			if err := exec.Command(c.Args[0], c.Args[1:]...).Run(); err != nil {
// 				t.Fatal(err)
// 			}
// 			at = i
// 		}
// 	}
// 	if at < 0 {
// 		t.Fatalf("no rm in %q", changes)
// 	}
// 	handOff := slices.IndexFunc(changes, func(s string) bool { return strings.HasPrefix(s, "sudo chmod -R ug+rwX") })
// 	clear := slices.IndexFunc(changes, func(s string) bool { return strings.Contains(s, " rm -f ") })
// 	start := slices.IndexFunc(changes, func(s string) bool { return strings.HasPrefix(s, launch) })
// 	if handOff < 0 || clear != handOff+1 || start <= clear {
// 		t.Errorf("order: hand-off %d, rm %d, launch %d in %q", handOff, clear, start, changes)
// 	}
// }

// // Code the server account leaves in Paper's library cache or remapped plugin
// // folder doesn't outlast a restart, in either mode, unless it's approved or
// // paper.jar's own. A link there is removed, not followed.
// func TestStartClearsCaches(t *testing.T) {
// 	defer testid.Line(t, "1a3jzy0")
// 	for _, linked := range []bool{false, true} {
// 		for _, mode := range []string{"docker", "native"} {
// 			var g *rig
// 			var start func() error
// 			launch := "sudo docker create "
// 			if mode == "docker" {
// 				g = newRig(t)
// 				g.states = []string{"", "running"}
// 				g.log = doneLine
// 				start = func() error { return g.d.Start(context.Background(), StartMode{}) }
// 			} else {
// 				n := newNativeRig(t)
// 				n.states = []string{"inactive", "active"}
// 				n.launch = func() { n.writeLog(t, doneLine) }
// 				g, launch = n.rig, "sudo systemd-run "
// 				start = func() error { return n.n.Start(context.Background(), StartMode{}) }
// 			}
// 			gone, kept := plantCaches(t, g.dir, g.d.PluginsFile, linked)
// 			if err := start(); err != nil {
// 				t.Fatalf("%s, linked %v: %v", mode, linked, err)
// 			}
// 			runClear(t, g, launch)
// 			if !strings.Contains(g.out.String(), "isn't approved, so Paper downloads") && !strings.Contains(g.out.String(), "aren't approved, so Paper downloads") {
// 				t.Errorf("%s, linked %v: no word of it in %q", mode, linked, g.out.String())
// 			}
// 			for _, f := range gone {
// 				if _, err := os.Lstat(filepath.Join(g.dir, f)); !errors.Is(err, os.ErrNotExist) {
// 					t.Errorf("%s, linked %v: %s is still there (%v)", mode, linked, f, err)
// 				}
// 			}
// 			for _, f := range kept {
// 				if _, err := os.Stat(filepath.Join(g.dir, f)); err != nil {
// 					t.Errorf("%s, linked %v: %s is gone: %v", mode, linked, f, err)
// 				}
// 			}
// 		}
// 	}
// }

// // A cache start can't clear stops the start before the launch, and the trees
// // go back. With nothing to clear, there's no rm.
// func TestStartClearFails(t *testing.T) {
// 	defer testid.Line(t, "1a3k014")
// 	g := newRig(t)
// 	g.states = []string{"", "running"}
// 	g.log = doneLine
// 	if err := g.d.Start(context.Background(), StartMode{}); err != nil {
// 		t.Fatal(err)
// 	}
// 	if got := g.changes(); slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, " rm ") }) {
// 		t.Errorf("nothing to clear, but ran %q", got)
// 	}

// 	g = newRig(t)
// 	plantCaches(t, g.dir, g.d.PluginsFile, false)
// 	g.r.Reply = func(c host.Cmd) ([]byte, error) {
// 		if c.Args[0] == "rm" {
// 			return nil, errors.New("rm: cannot remove: Permission denied")
// 		}
// 		return g.reply(c)
// 	}
// 	err := g.d.Start(context.Background(), StartMode{})
// 	got := g.changes()
// 	if err == nil || !strings.Contains(err.Error(), "clearing Paper's library cache") ||
// 		slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "docker create") }) ||
// 		!strings.Contains(got[len(got)-1], "chown -R -- alice:minecraftd") {
// 		t.Errorf("err %v, ran %q", err, got)
// 	}
// }
