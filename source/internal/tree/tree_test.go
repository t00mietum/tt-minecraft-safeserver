// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package tree

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// deepDir makes a folder at least 3 levels deep, whatever TMPDIR is.
func deepDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "srv", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func testConfig(trees ...string) *config.Config {
	return &config.Config{RunUser: "minecraftd", RunGroup: "mcgroup", OwnerAfterStop: "alice", OwnerTrees: trees}
}

func TestToServer(t *testing.T) {
	defer testid.Line(t, "df5xf")
	server, world := deepDir(t, "server"), deepDir(t, "world")
	missing := filepath.Join(filepath.Dir(server), "gone")
	var r host.Fake
	warns, err := ToServer(context.Background(), &r, testConfig(server, missing, world))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], missing) {
		t.Errorf("warns = %q, want one for %s", warns, missing)
	}
	want := []string{
		"sudo chown -R -- minecraftd:mcgroup " + host.Quote([]string{server}),
		"sudo chmod -R ug+rwX,o-rwx -- " + host.Quote([]string{server}),
		"sudo chown -R -- minecraftd:mcgroup " + host.Quote([]string{world}),
		"sudo chmod -R ug+rwX,o-rwx -- " + host.Quote([]string{world}),
	}
	if got := r.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Back to the operator with the group kept. With no operator, as from the
// boot unit, nothing changes hands.
func TestToOperator(t *testing.T) {
	defer testid.Line(t, "df5xg")
	server := deepDir(t, "server")
	var r host.Fake
	c := testConfig(server)
	if _, err := ToOperator(context.Background(), &r, c); err != nil {
		t.Fatal(err)
	}
	want := []string{"sudo chown -R -- alice:mcgroup " + host.Quote([]string{server})}
	if got := r.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}

	r = host.Fake{}
	c.OwnerAfterStop = ""
	warns, err := ToOperator(context.Background(), &r, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Ran) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "ownerAfterStop") {
		t.Errorf("ran %q, warns %q; want nothing run and one warning", r.Lines(), warns)
	}
}

// A shallow tree is refused, and so is a link to one, before anything runs.
func TestShallowTrees(t *testing.T) {
	defer testid.Line(t, "df5xh")
	base := deepDir(t, "links")
	toShallow := filepath.Join(base, "to-usr")
	if err := os.Symlink("/usr", toShallow); err != nil {
		t.Fatal(err)
	}
	for _, tree := range []string{"/", "/srv", "/srv/x", "srv/x/y", "/srv/x/../y", toShallow} {
		var r host.Fake
		if _, err := ToServer(context.Background(), &r, testConfig(tree)); err == nil || len(r.Ran) != 0 {
			t.Errorf("%s: err %v, ran %q; want refused", tree, err, r.Lines())
		}
	}

	server := deepDir(t, "server")
	link := filepath.Join(base, "server")
	if err := os.Symlink(server, link); err != nil {
		t.Fatal(err)
	}
	var r host.Fake
	if _, err := ToOperator(context.Background(), &r, testConfig(link)); err != nil {
		t.Fatal(err)
	}
	if got := r.Lines(); len(got) != 1 || !strings.HasSuffix(got[0], " "+host.Quote([]string{server})) {
		t.Errorf("ran %q, want the link's target %s", got, server)
	}
}
