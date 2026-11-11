// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// noDefaultConfig points the default config at a file that isn't there.
func noDefaultConfig(t *testing.T) {
	t.Helper()
	old := defaultConfig
	defaultConfig = filepath.Join(t.TempDir(), "config.shcl")
	t.Cleanup(func() { defaultConfig = old })
}

func runArgs(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHelpAboutVersion(t *testing.T) {
	defer testid.Line(t, "df5xj")
	noDefaultConfig(t)
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"start ", "stop ", "status ", "watch ", "--config PATH", "--set KEY=VALUE"}},
		{[]string{"-h"}, []string{"Commands:"}},
		{[]string{"start", "--help"}, []string{"--native ", "--debug ", "--dry-run ", "--foreground ", "--set KEY=VALUE"}},
		{[]string{"stop", "--help"}, []string{"--warn N", "--dry-run "}},
		{[]string{"watch", "--help"}, []string{"--interval N"}},
		{[]string{"--about"}, []string{"GPL v2 or later"}},
		{[]string{"--version"}, []string{"tt-minecraftd " + version + " (build "}},
	} {
		code, out, errOut := runArgs(c.args...)
		if code != exitOK || errOut != "" {
			t.Errorf("%q: exit %d, stderr %q", c.args, code, errOut)
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%q: output lacks %q:\n%s", c.args, w, out)
			}
		}
		if c.args[0] != "--version" && (!strings.HasPrefix(out, "\n") || !strings.HasSuffix(out, "\n\n")) {
			t.Errorf("%q: want a blank line at the start and end", c.args)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	defer testid.Line(t, "df5xk")
	noDefaultConfig(t)
	for _, args := range [][]string{
		{},
		{"launch"},
		{"start", "--bogus"},
		{"start", "now"},
		{"start", "--debug", "--foreground"},
		{"stop", "--native"},
		{"stop", "--warn", "-1"},
		{"stop", "--warn", "soon"},
		{"watch", "--interval", "0"},
		{"status", "--set", "heapMax"},
		{"status", "--set", "=6G"},
		{"status", "--set", "noSuchKey=1"},
		{"status", "--set", "runMode=podman"},
		{"--config"},
	} {
		code, _, errOut := runArgs(args...)
		if code != exitUsage || errOut == "" {
			t.Errorf("%q: exit %d, stderr %q; want %d and a message", args, code, errOut, exitUsage)
		}
	}
}

// Options reach the config in the order given, so the last one wins, and the
// common ones work before the command too.
func TestOptionsReachConfig(t *testing.T) {
	defer testid.Line(t, "df5xm")
	t.Setenv("SUDO_USER", "")
	t.Setenv("USER", "alice")
	noDefaultConfig(t)
	path := filepath.Join(t.TempDir(), "my.shcl")
	if err := os.WriteFile(path, []byte("heapMax: 2G\nrunUser: mcfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args              []string
		mode, user, group string
		heap              int64
	}{
		{[]string{"start"}, "docker", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"start", "--native"}, "native", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"start", "--native", "--set", "runMode=docker"}, "docker", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"start", "--set", "runMode=docker", "--native"}, "native", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"--set", "heapMax=6G", "start", "--set", "runUser=mc"}, "docker", "mc", "mc", 6 << 30},
		{[]string{"--config", path, "status"}, "docker", "mcfile", "mcfile", 2 << 30},
		{[]string{"status", "--config=" + path, "--set", "heapMax=3G"}, "docker", "mcfile", "mcfile", 3 << 30},
	} {
		var errOut bytes.Buffer
		inv, code, done := prepare(c.args, &bytes.Buffer{}, &errOut)
		if done {
			t.Errorf("%q: stopped with exit %d: %s", c.args, code, errOut.String())
			continue
		}
		cfg := inv.cfg
		if cfg.RunMode != c.mode || cfg.RunUser != c.user || cfg.RunGroup != c.group || cfg.HeapMax != c.heap || cfg.OwnerAfterStop != "alice" {
			t.Errorf("%q: got mode %s, user %s:%s, heap %d, owner %s", c.args, cfg.RunMode, cfg.RunUser, cfg.RunGroup, cfg.HeapMax, cfg.OwnerAfterStop)
		}
	}
}

// A missing default file means the defaults, with a warning. A missing file
// named by --config is an error.
func TestMissingConfig(t *testing.T) {
	defer testid.Line(t, "df5xn")
	noDefaultConfig(t)
	var errOut bytes.Buffer
	inv, _, done := prepare([]string{"status"}, &bytes.Buffer{}, &errOut)
	if done || inv.cfg.RunMode != "docker" || !strings.Contains(errOut.String(), "defaults are used") {
		t.Errorf("default missing: done %v, mode %q, stderr %q", done, inv.cfg.RunMode, errOut.String())
	}
	code, _, msg := runArgs("status", "--config", filepath.Join(t.TempDir(), "nope.shcl"))
	if code != exitError || !strings.Contains(msg, "nope.shcl") {
		t.Errorf("--config missing: exit %d, stderr %q; want %d", code, msg, exitError)
	}
	for _, args := range [][]string{{"status"}} {
		if code, _, msg := runArgs(args...); code != exitError || !strings.Contains(msg, "isn't built yet") {
			t.Errorf("%q: exit %d, stderr %q", args, code, msg)
		}
	}
}
