// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
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
