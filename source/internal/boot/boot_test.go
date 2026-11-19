// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// The unit follows design.md's Boot: start --foreground and stop, a stop
// timeout past stopTimeout, the network first, docker.service in Docker
// mode, and the firewall unit required when the firewall is on.
func TestUnit(t *testing.T) {
	defer testid.Line(t, "df78p00")
	c := config.Defaults()
	c.RunMode, c.Firewall, c.StopTimeout = "docker", true, 120*time.Second
	u := Unit(&c, config.DefaultPath, LocalBin)
	for _, want := range []string{
		"\nExecStart=/usr/local/bin/tt-minecraftd start --foreground\n",
		"\nExecStop=/usr/local/bin/tt-minecraftd stop\n",
		"\nTimeoutStopSec=165\n",
		"\nAfter=network-online.target\n",
		"\nAfter=docker.service\n",
		"\nRequires=tt-minecraftd-firewall.service\n",
		"\nWantedBy=multi-user.target\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit lacks %q:\n%s", want, u)
		}
	}
	if u := Unit(&c, config.DefaultPath, PkgBin); !strings.Contains(u, "\nExecStart=/usr/bin/tt-minecraftd start --foreground\n") || !strings.Contains(u, "\nExecStop=/usr/bin/tt-minecraftd stop\n") {
		t.Errorf("the package's binary isn't what it runs:\n%s", u)
	}
	c.RunMode, c.Firewall = "podman", false
	u = Unit(&c, "/etc/other dir/50%$x\".shcl", LocalBin)
	if strings.Contains(u, "docker") || strings.Contains(u, "firewall") {
		t.Errorf("podman, firewall off:\n%s", u)
	}
	if want := `ExecStart=/usr/local/bin/tt-minecraftd --config "/etc/other dir/50%%$$x\".shcl" start --foreground`; !strings.Contains(u, want+"\n") {
		t.Errorf("unit lacks %q:\n%s", want, u)
	}
	// systemd's own check, where it's installed.
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		return
	}
	dir := t.TempDir()
	file := filepath.Join(dir, UnitName)
	c.RunMode, c.Firewall = "docker", false
	if err := os.WriteFile(file, []byte(Unit(&c, config.DefaultPath, LocalBin)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("systemd-analyze", "verify", file).CombinedOutput() // #nosec G204 -- a test's own file
	for line := range strings.Lines(string(out)) {
		// The binary isn't installed here, and docker.service may not be.
		if strings.Contains(line, UnitName) && !strings.Contains(line, "/usr/local/bin/tt-minecraftd") && !strings.Contains(line, "docker.service") {
			t.Errorf("systemd-analyze verify: %s", line)
		}
	}
}

// Inside goes by the unit's cgroup. Serves needs the unit enabled, and its
// ExecStart for this config.
func TestInsideAndServes(t *testing.T) {
	defer testid.Line(t, "df78q00")
	for cg, want := range map[string]bool{
		"0::/system.slice/tt-minecraftd.service\n":         true,
		"0::/system.slice/tt-minecraftd-server.service\n":  false,
		"0::/user.slice/user-1001.slice/session-3.scope\n": false,
		"": false,
	} {
		if Inside(cg) != want {
			t.Errorf("Inside(%q) isn't %v", cg, want)
		}
	}
	enabled := "enabled\n"
	f := &host.Fake{Reply: func(host.Cmd) ([]byte, error) { return []byte(enabled), nil }}
	file := filepath.Join(t.TempDir(), UnitName)
	ctx := context.Background()
	c := config.Defaults()
	serves := func(path string) bool {
		t.Helper()
		ok, err := Serves(ctx, f, file, path)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if serves(config.DefaultPath) {
		t.Error("no unit file, but it serves")
	}
	// A unit from before the package came still runs setup's copy.
	for _, bin := range []string{PkgBin, LocalBin} {
		if err := os.WriteFile(file, []byte(Unit(&c, config.DefaultPath, bin)), 0o600); err != nil {
			t.Fatal(err)
		}
		if !serves(config.DefaultPath) || serves("/etc/other.shcl") {
			t.Errorf("%s: serves the wrong config", bin)
		}
	}
	if err := os.WriteFile(file, []byte(Unit(&c, config.DefaultPath, "/opt/tt-minecraftd")), 0o600); err != nil {
		t.Fatal(err)
	}
	if serves(config.DefaultPath) {
		t.Error("serves from a path no install uses")
	}
	enabled = "disabled\n"
	if serves(config.DefaultPath) {
		t.Error("disabled, but it serves")
	}
}

// Owner reads back the config any unit Unit writes runs, however odd its
// path, and nothing from a unit it didn't write. See design.md, Boot.
func TestOwner(t *testing.T) {
	defer testid.Line(t, "df7a600")
	c := config.Defaults()
	rel, err := filepath.Abs("rel/c.shcl")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		config.DefaultPath:              config.DefaultPath,
		"":                              config.DefaultPath,
		"/srv/mc/config.shcl":           "/srv/mc/config.shcl",
		"/etc/other dir/50%$x\".shcl":   "/etc/other dir/50%$x\".shcl",
		`/a\b\\"c "start --foreground"`: `/a\b\\"c "start --foreground"`,
		"/x/$$%%/\"\"":                  "/x/$$%%/\"\"",
		"rel/c.shcl":                    rel,
	} {
		for _, bin := range []string{PkgBin, LocalBin} {
			got, ok := Owner(Unit(&c, path, bin))
			if !ok || got != want {
				t.Errorf("Owner of the unit for %q from %s: %q, %v, want %q", path, bin, got, ok, want)
			}
		}
		got, _ := Owner(Unit(&c, path, LocalBin))
		if !SameConfig(got, path) {
			t.Errorf("SameConfig(%q, %q) is false", got, path)
		}
	}
	for _, unit := range []string{
		"",
		"[Service]\nExecStart=/usr/bin/java -jar paper.jar\n",
		"ExecStart=" + LocalBin + " --config /etc/x.shcl start --foreground\n",
		"ExecStart=" + PkgBin + ` --config "/a"b" start --foreground` + "\n",
		"ExecStart=" + LocalBin + ` --config "/a%b" start --foreground` + "\n",
		"ExecStart=" + PkgBin + ` --config "/a\" start --foreground` + "\n",
		"ExecStart=" + LocalBin + " start\n",
		"ExecStart=/opt/tt-minecraftd start --foreground\n",
	} {
		if got, ok := Owner(unit); ok {
			t.Errorf("Owner(%q) is %q", unit, got)
		}
	}
	if !SameConfig("/etc/tt-minecraftd/../tt-minecraftd/config.shcl", "") || SameConfig("/etc/other.shcl", config.DefaultPath) {
		t.Error("SameConfig")
	}
}

// The unit runs the package's binary when it's there, so a package upgrade
// reaches it, and setup's copy otherwise. See design.md, Boot.
func TestBin(t *testing.T) {
	defer testid.Line(t, "df7as00")
	dir := t.TempDir()
	pkg := filepath.Join(dir, "tt-minecraftd")
	if got := Bin(pkg); got != LocalBin {
		t.Errorf("no package: %s", got)
	}
	if err := os.Mkdir(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := Bin(pkg); got != LocalBin {
		t.Errorf("a folder in its place: %s", got)
	}
	if err := os.Remove(pkg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Bin(pkg); got != PkgBin {
		t.Errorf("package there: %s", got)
	}
	if PkgBin != "/usr/bin/tt-minecraftd" || LocalBin != "/usr/local/bin/tt-minecraftd" || Bins[0] != PkgBin {
		t.Errorf("paths: %s, %s, %q", PkgBin, LocalBin, Bins)
	}
}
