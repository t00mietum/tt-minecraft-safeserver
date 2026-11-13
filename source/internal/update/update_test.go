// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

type fakePaper struct {
	newest    int
	asked     []string
	installed []string
	err       error
}

func (p *fakePaper) NewestVersion(context.Context) (string, error) {
	p.asked = append(p.asked, "newest")
	return "26.3", p.err
}

func (p *fakePaper) NewestStable(_ context.Context, v string) (paper.Build, error) {
	p.asked = append(p.asked, v)
	name := "paper-" + v + "-" + strconv.Itoa(p.newest) + ".jar"
	return paper.Build{Version: v, ID: p.newest, Name: name, SHA256: strings.Repeat("cd", 32), Size: 3}, p.err
}

func (p *fakePaper) Java(_ context.Context, v string) (int, error) {
	p.asked = append(p.asked, "java "+v)
	return 25, p.err
}

func (p *fakePaper) Install(_ context.Context, dir string, b paper.Build) error {
	p.installed = append(p.installed, b.Name)
	if err := os.WriteFile(filepath.Join(dir, b.Name), []byte("new"), 0o600); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, "paper.jar"))
	return os.Symlink(b.Name, filepath.Join(dir, "paper.jar"))
}

type rig struct {
	u   *Update
	p   *fakePaper
	dir string
	out *bytes.Buffer
}

// newRig has builds 240 and 244 on disk, with paper.jar at 244.
func newRig(t *testing.T, answers string, newest int) *rig {
	t.Helper()
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	if err := os.Mkdir(server, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"paper-26.3-240.jar", "paper-26.3-244.jar"} {
		if err := os.WriteFile(filepath.Join(server, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("paper-26.3-244.jar", filepath.Join(server, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	r := &rig{p: &fakePaper{newest: newest}, dir: server, out: &bytes.Buffer{}}
	r.u = &Update{
		Cfg:         &config.Config{ServerDir: server},
		PluginsFile: filepath.Join(dir, "plugins.shcl"),
		In:          bufio.NewScanner(strings.NewReader(answers)),
		Out:         r.out,
		Paper:       r.p,
	}
	return r
}

func (r *rig) files() []string {
	var names []string
	entries, _ := os.ReadDir(r.dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// --paper stays on the Minecraft version, asks, approves the new build, and
// keeps the last 2.
func TestPaper(t *testing.T) {
	defer testid.Line(t, "df66z")
	ctx := context.Background()

	r := newRig(t, "y\n", 249)
	if err := r.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if !reflect.DeepEqual(r.p.asked, []string{"26.3"}) || !reflect.DeepEqual(r.p.installed, []string{"paper-26.3-249.jar"}) {
		t.Errorf("asked %q, installed %q", r.p.asked, r.p.installed)
	}
	if !reflect.DeepEqual(r.files(), []string{"paper-26.3-244.jar", "paper-26.3-249.jar", "paper.jar"}) {
		t.Errorf("left %q\n%s", r.files(), r.out)
	}
	for _, want := range []string{"from build 244 to build 249", "Go ahead?", "approved", "older builds removed: paper-26.3-240.jar", "next start"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("no %q:\n%s", want, r.out)
		}
	}
	list, _, err := plugins.ReadApproved(r.u.PluginsFile)
	if err != nil || len(list) != 1 || list[0].Name != "paper-26.3-249.jar" || list[0].SHA256 != strings.Repeat("cd", 32) {
		t.Errorf("approved %+v %v", list, err)
	}

	r = newRig(t, "n\n", 249)
	if err := r.u.Do(ctx); err == nil || len(r.p.installed) > 0 || len(r.files()) != 3 {
		t.Errorf("a no went on: %v, installed %q", err, r.p.installed)
	}

	r = newRig(t, "", 249)
	r.u.DryRun = true
	if err := r.u.Do(ctx); err != nil || len(r.p.installed) > 0 || !strings.Contains(r.out.String(), "would download paper-26.3-249.jar") {
		t.Errorf("dry run: %v, installed %q\n%s", err, r.p.installed, r.out)
	}

	r = newRig(t, "", 244)
	if err := r.u.Do(ctx); err != nil || len(r.p.installed) > 0 || !strings.Contains(r.out.String(), "OK      Paper 26.3 build 244 is the newest") {
		t.Errorf("up to date: %v, installed %q\n%s", err, r.p.installed, r.out)
	}

	// The link's build is gone, so the same build comes again.
	r = newRig(t, "", 244)
	r.u.Yes = true
	if err := os.Remove(filepath.Join(r.dir, "paper-26.3-244.jar")); err != nil {
		t.Fatal(err)
	}
	if err := r.u.Do(ctx); err != nil || !reflect.DeepEqual(r.p.installed, []string{"paper-26.3-244.jar"}) || !strings.Contains(r.out.String(), "which is gone") {
		t.Errorf("gone: %v, installed %q\n%s", err, r.p.installed, r.out)
	}

	// No paper.jar takes the newest release, as setup does.
	r = newRig(t, "", 249)
	r.u.Yes = true
	if err := os.Remove(filepath.Join(r.dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if err := r.u.Do(ctx); err != nil || !reflect.DeepEqual(r.p.asked, []string{"newest", "26.3"}) || len(r.p.installed) != 1 {
		t.Errorf("no paper.jar: %v, asked %q, installed %q", err, r.p.asked, r.p.installed)
	}

	r = newRig(t, "y\n", 249)
	r.p.err = errors.New("no network")
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "can't get Paper") || len(r.p.installed) > 0 || strings.Contains(r.out.String(), "Go ahead") {
		t.Errorf("failed lookup: %v, installed %q\n%s", err, r.p.installed, r.out)
	}

	r = newRig(t, "y\n", 249)
	r.u.Cfg.ServerDir = filepath.Join(r.dir, "nope")
	if err := r.u.Do(ctx); err == nil || len(r.p.asked) > 0 {
		t.Errorf("no serverDir: %v, asked %q", err, r.p.asked)
	}
}

// fakeGeyser has Geyser 2.1 build 9, and writes "new" for it.
type fakeGeyser struct {
	asked     []string
	installed []string
	name      string
	err       error
}

func (g *fakeGeyser) Latest(_ context.Context, p string) (geyser.Build, error) {
	g.asked = append(g.asked, p)
	name := g.name
	if name == "" {
		name = map[string]string{"geyser": "Geyser-Spigot.jar", "floodgate": "floodgate-spigot.jar"}[p]
	}
	return geyser.Build{Project: p, Version: "2.1", ID: 9, Name: name, SHA256: sumOf("new")}, g.err
}

func (g *fakeGeyser) Install(_ context.Context, dir string, b geyser.Build) error {
	g.installed = append(g.installed, b.Name)
	return os.WriteFile(filepath.Join(dir, "plugins", b.Name), []byte("new"), 0o600)
}

func (g *fakeGeyser) Source(p string) string { return "https://example.com/" + p + "/latest" }

func sumOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// pluginRig has Geyser approved with its source, Floodgate approved at the
// newest build, and a plugin with a source update doesn't know.
func pluginRig(t *testing.T, answers string) (*rig, *fakeGeyser) {
	t.Helper()
	r := newRig(t, answers, 244)
	g := &fakeGeyser{}
	r.u.Geyser = g
	r.u.Parts = Parts{Plugins: true}
	if err := os.Mkdir(filepath.Join(r.dir, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	// A slice, not a map: update looks them up in the file's order, and the
	// failed lookup test expects Geyser's error first.
	jars := []struct{ name, text, src string }{
		{"Geyser-Spigot.jar", "old", g.Source("geyser")},
		{"floodgate-spigot.jar", "new", g.Source("floodgate")},
		{"Thing.jar", "thing", "https://modrinth.example/thing"},
	}
	var infos []plugins.Info
	for _, j := range jars {
		if err := os.WriteFile(filepath.Join(r.dir, "plugins", j.name), []byte(j.text), 0o600); err != nil {
			t.Fatal(err)
		}
		infos = append(infos, plugins.Info{Jar: plugins.Jar{Name: "plugins/" + j.name}, SHA256: sumOf(j.text), Source: j.src})
	}
	if err := plugins.Approve(r.u.PluginsFile, infos); err != nil {
		t.Fatal(err)
	}
	return r, g
}

// --plugins updates what has a known source and a newer build, keeps the
// source, and leaves the rest.
func TestPlugins(t *testing.T) {
	defer testid.Line(t, "df672")
	ctx := context.Background()

	r, g := pluginRig(t, "y\n")
	if err := r.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if !reflect.DeepEqual(g.installed, []string{"Geyser-Spigot.jar"}) || len(r.p.asked) > 0 {
		t.Errorf("installed %q, Paper asked %q", g.installed, r.p.asked)
	}
	for _, want := range []string{"Update plugins/Geyser-Spigot.jar from an older build to Geyser 2.1 build 9", "OK      plugins/floodgate-spigot.jar is the newest build", "SKIPPED plugins/Thing.jar: its source isn't one update knows", "FIXED   plugins/Geyser-Spigot.jar approved"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("no %q:\n%s", want, r.out)
		}
	}
	list, _, err := plugins.ReadApproved(r.u.PluginsFile)
	if err != nil || len(list) != 3 {
		t.Fatalf("approved %+v %v", list, err)
	}
	for _, a := range list {
		if a.Name == "plugins/Geyser-Spigot.jar" && (a.SHA256 != sumOf("new") || a.Source != g.Source("geyser")) {
			t.Errorf("Geyser's entry %+v", a)
		}
	}

	r, g = pluginRig(t, "n\n")
	if err := r.u.Do(ctx); err == nil || len(g.installed) > 0 {
		t.Errorf("a no went on: %v, installed %q", err, g.installed)
	}

	r, g = pluginRig(t, "")
	r.u.DryRun = true
	if err := r.u.Do(ctx); err != nil || len(g.installed) > 0 || !strings.Contains(r.out.String(), "would download Geyser-Spigot.jar") {
		t.Errorf("dry run: %v, installed %q\n%s", err, g.installed, r.out)
	}

	// A new name would leave 2 Geysers in plugins/.
	r, g = pluginRig(t, "y\n")
	g.name = "Geyser-Paper.jar"
	if err := r.u.Do(ctx); err != nil || len(g.installed) > 0 || !strings.Contains(r.out.String(), "is named Geyser-Paper.jar") {
		t.Errorf("renamed: %v, installed %q\n%s", err, g.installed, r.out)
	}

	r, g = pluginRig(t, "y\n")
	if err := os.Remove(filepath.Join(r.dir, "plugins", "Geyser-Spigot.jar")); err != nil {
		t.Fatal(err)
	}
	if err := r.u.Do(ctx); err != nil || len(g.installed) > 0 || !strings.Contains(r.out.String(), "not in the server folder") {
		t.Errorf("removed: %v, installed %q\n%s", err, g.installed, r.out)
	}

	r, g = pluginRig(t, "y\n")
	g.err = errors.New("no network")
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "can't get Geyser") || len(g.installed) > 0 {
		t.Errorf("failed lookup: %v, installed %q", err, g.installed)
	}

	// With no part named, Paper and the plugins share one plan.
	r, g = pluginRig(t, "y\n")
	r.p.newest = 249
	r.u.Parts = Parts{}
	if err := r.u.Do(ctx); err != nil || len(g.installed) != 1 || len(r.p.installed) != 1 || strings.Count(r.out.String(), "Go ahead?") != 1 {
		t.Errorf("both: %v, installed %q and %q\n%s", err, g.installed, r.p.installed, r.out)
	}
}

// --mc-version asks even with --yes, snapshots before anything changes, and
// won't go back to an older version.
func TestMCVersion(t *testing.T) {
	defer testid.Line(t, "df78m")
	ctx := context.Background()
	snap := "zfs snapshot -- tank/mc@20261112-110000_pre_tt-minecraftd-mc-version"
	move := func(answers, dataset string) (*rig, *host.Fake) {
		r := newRig(t, answers, 12)
		f := &host.Fake{}
		r.u.Run, r.u.Yes, r.u.MCVersion = f, true, "26.4"
		r.u.Cfg.ZfsDataset = dataset
		r.u.Now = func() time.Time { return time.Date(2026, 11, 12, 11, 0, 0, 0, time.Local) }
		return r, f
	}

	r, f := move("n\n", "tank/mc")
	if err := r.u.Do(ctx); err == nil || len(r.p.installed) > 0 || len(f.Ran) > 0 || !strings.Contains(r.out.String(), "Go ahead, and move to 26.4?") {
		t.Errorf("--yes covered it: %v, installed %q, ran %q\n%s", err, r.p.installed, f.Lines(), r.out)
	}

	r, f = move("y\n", "tank/mc")
	var installedAtSnap = -1
	f.Reply = func(c host.Cmd) ([]byte, error) {
		if c.Args[1] == "snapshot" {
			installedAtSnap = len(r.p.installed)
		}
		return nil, nil
	}
	if err := r.u.Do(ctx); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if lines := f.Lines(); len(lines) == 0 || lines[0] != snap || installedAtSnap != 0 || !reflect.DeepEqual(r.p.installed, []string{"paper-26.4-12.jar"}) {
		t.Errorf("ran %q, %d installed at the snapshot, installed %q", lines, installedAtSnap, r.p.installed)
	}
	if !reflect.DeepEqual(r.files(), []string{"paper-26.3-244.jar", "paper-26.4-12.jar", "paper.jar"}) || !strings.Contains(r.out.String(), "  - Take a ZFS snapshot of tank/mc first.\n  - Move Paper from Minecraft 26.3 build 244 to Minecraft 26.4 build 12") {
		t.Errorf("left %q\n%s", r.files(), r.out)
	}

	r, f = move("y\n", "tank/mc")
	f.Reply = func(c host.Cmd) ([]byte, error) {
		if c.Args[1] == "snapshot" {
			return nil, errors.New("out of space")
		}
		return nil, nil
	}
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "can't snapshot tank/mc") || len(r.p.installed) > 0 {
		t.Errorf("failed snapshot: %v, installed %q", err, r.p.installed)
	}

	r, f = move("y\n", "")
	if err := r.u.Do(ctx); err != nil || len(f.Ran) > 0 || len(r.p.installed) != 1 || !strings.Contains(r.out.String(), "zfsDataset isn't set") {
		t.Errorf("no dataset: %v, ran %q, installed %q\n%s", err, f.Lines(), r.p.installed, r.out)
	}

	r, _ = move("", "tank/mc")
	r.u.DryRun = true
	if err := r.u.Do(ctx); err != nil || len(r.p.installed) > 0 || strings.Contains(r.out.String(), "Go ahead") || !strings.Contains(r.out.String(), "would download paper-26.4-12.jar") {
		t.Errorf("dry run: %v, installed %q\n%s", err, r.p.installed, r.out)
	}

	r, f = move("y\n", "tank/mc")
	r.u.MCVersion = "1.21.11"
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "can't go back to 1.21.11") || len(r.p.asked) > 0 || len(f.Ran) > 0 {
		t.Errorf("older: %v, asked %q, ran %q", err, r.p.asked, f.Lines())
	}

	// The version it's on already is a plain --paper.
	r, f = move("", "tank/mc")
	r.u.MCVersion = "26.3"
	r.p.newest = 249
	if err := r.u.Do(ctx); err != nil || len(f.Ran) > 0 || !reflect.DeepEqual(r.p.installed, []string{"paper-26.3-249.jar"}) || strings.Contains(r.out.String(), "move to") {
		t.Errorf("same version: %v, ran %q, installed %q\n%s", err, f.Lines(), r.p.installed, r.out)
	}

	r, _ = move("y\n", "tank/mc")
	r.p.err = paper.ErrNoStable
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "can't get Paper for Minecraft 26.4") {
		t.Errorf("no build: %v", err)
	}

	for _, c := range []struct {
		parts   Parts
		version string
	}{{Parts{Plugins: true}, "26.4"}, {Parts{Paper: true}, "26.4/../x"}} {
		r, _ = move("y\n", "tank/mc")
		r.u.Parts, r.u.MCVersion = c.parts, c.version
		if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "--mc-version") || len(r.p.asked) > 0 {
			t.Errorf("%+v %q: %v, asked %q", c.parts, c.version, err, r.p.asked)
		}
	}
}
