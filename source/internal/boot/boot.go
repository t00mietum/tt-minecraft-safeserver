// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package boot is the unit that starts the server at boot, with startAtBoot
// on. See design.md, Boot.
package boot

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/firewall"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

const (
	UnitName = "tt-minecraftd.service"
	UnitFile = "/etc/systemd/system/" + UnitName
	// Bin is where setup installs the tool, and what the unit runs.
	Bin = "/usr/local/bin/tt-minecraftd"
	// stopMargin is past stopTimeout: stop's 30 seconds from SIGTERM to
	// SIGKILL, and some to hand back after.
	stopMargin = 45 * time.Second
)

// Command is the tool with its --config, when that isn't the default, in
// systemd's quoting.
func Command(configPath string) string {
	if configPath == "" || configPath == config.DefaultPath {
		return Bin
	}
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	return Bin + " --config " + quote(configPath)
}

// quote is one argument for an Exec line. systemd expands % specifiers and $
// variables there, even inside quotes.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$")
	return `"` + r.Replace(s) + `"`
}

// ExecStart is the unit's ExecStart line, which also tells whose config the
// unit serves.
func ExecStart(configPath string) string {
	return "ExecStart=" + Command(configPath) + " start --foreground"
}

// Unit is tt-minecraftd.service for the config at configPath.
func Unit(c *config.Config, configPath string) string {
	var b strings.Builder
	b.WriteString(`[Unit]
Description=tt-minecraftd Minecraft server
Documentation=https://github.com/t00mietum/tt-minecraft-safeserver
Wants=network-online.target
After=network-online.target
`)
	if c.RunMode == "docker" {
		b.WriteString("Wants=docker.service\nAfter=docker.service\n")
	}
	if c.Firewall {
		// Never up at boot without the rules.
		b.WriteString("Requires=" + firewall.UnitName + "\nAfter=" + firewall.UnitName + "\n")
	}
	b.WriteString(`
[Service]
Type=simple
` + ExecStart(configPath) + `
ExecStop=` + Command(configPath) + ` stop
TimeoutStopSec=` + strconv.Itoa(int((c.StopTimeout+stopMargin)/time.Second)) + `

[Install]
WantedBy=multi-user.target
`)
	return b.String()
}

// Inside says whether cgroup, /proc/self/cgroup's text, puts the process in
// the boot unit, as start --foreground and its ExecStop are.
func Inside(cgroup string) bool {
	for line := range strings.Lines(cgroup) {
		line = strings.TrimSpace(line)
		if i := strings.LastIndexByte(line, '/'); i >= 0 && line[i+1:] == UnitName {
			return true
		}
	}
	return false
}

// InsideNow is Inside for this process.
func InsideNow() bool {
	b, err := os.ReadFile("/proc/self/cgroup")
	return err == nil && Inside(string(b))
}

// Serves says whether the boot unit is enabled and runs the config at
// configPath, so start and stop for that config go through systemctl.
// unitFile empty means the real one.
func Serves(ctx context.Context, r host.Runner, unitFile, configPath string) (bool, error) {
	if unitFile == "" {
		unitFile = UnitFile
	}
	text, err := os.ReadFile(unitFile) // #nosec G304 -- the boot unit's path
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	want := ExecStart(configPath)
	found := false
	for line := range strings.Lines(string(text)) {
		found = found || strings.TrimSpace(line) == want
	}
	if !found {
		return false, nil
	}
	out, _ := r.Run(ctx, host.Cmd{Look: true, Args: []string{"systemctl", "is-enabled", UnitName}}) // disabled exits 1
	return strings.TrimSpace(string(out)) == "enabled", nil
}
