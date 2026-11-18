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
	// UnitName is the boot unit, and UnitFile is where setup writes it.
	UnitName = "tt-minecraftd.service"
	UnitFile = "/etc/systemd/system/" + UnitName
	// Bin is where setup, install.bash, and the packages put the tool, and
	// what the unit runs. The package step checks the build names only this.
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

// unquote undoes quote. false when s isn't something quote gives.
func unquote(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", false
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c == '\\' || c == '%' || c == '$':
			if i+1 == len(inner) {
				return "", false
			}
			next := inner[i+1]
			if (c == '\\' && next != '\\' && next != '"') || (c != '\\' && next != c) {
				return "", false
			}
			b.WriteByte(next)
			i++
		case c == '"':
			return "", false
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}

// ExecStart is the unit's ExecStart line, which also tells whose config the
// unit serves.
func ExecStart(configPath string) string {
	return "ExecStart=" + Command(configPath) + " start --foreground"
}

// Owner is the config a unit's ExecStart runs, read back from what
// ExecStart writes. false when there's no such line.
func Owner(unit string) (string, bool) {
	for line := range strings.Lines(unit) {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart="+Bin)
		if !ok {
			continue
		}
		if rest, ok = strings.CutSuffix(rest, " start --foreground"); !ok {
			continue
		}
		if rest == "" {
			return config.DefaultPath, true
		}
		if rest, ok = strings.CutPrefix(rest, " --config "); !ok {
			continue
		}
		if path, ok := unquote(rest); ok {
			return path, true
		}
	}
	return "", false
}

// SameConfig says whether 2 config paths name the same file, the way
// Command and ExecStart see them. Empty is the default config.
func SameConfig(a, b string) bool {
	abs := func(p string) string {
		if p == "" {
			return config.DefaultPath
		}
		if full, err := filepath.Abs(p); err == nil {
			return full
		}
		return filepath.Clean(p)
	}
	return abs(a) == abs(b)
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
