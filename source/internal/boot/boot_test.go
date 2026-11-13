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
	defer testid.Line(t, "df78p")
	c := config.Defaults()
	c.RunMode, c.Firewall, c.StopTimeout = "docker", true, 120*time.Second
	u := Unit(&c, config.DefaultPath)
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
	c.RunMode, c.Firewall = "podman", false
	u = Unit(&c, "/etc/other dir/50%$x\".shcl")
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
	if err := os.WriteFile(file, []byte(Unit(&c, config.DefaultPath)), 0o600); err != nil {
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
	defer testid.Line(t, "df78q")
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
	if err := os.WriteFile(file, []byte(Unit(&c, config.DefaultPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !serves(config.DefaultPath) || serves("/etc/other.shcl") {
		t.Error("serves the wrong config")
	}
	enabled = "disabled\n"
	if serves(config.DefaultPath) {
		t.Error("disabled, but it serves")
	}
}
