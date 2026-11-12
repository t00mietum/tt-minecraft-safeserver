// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	repairpkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/repair"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/status"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// noDefaultConfig points the default config at a file that isn't there.
func noDefaultConfig(t *testing.T) {
	t.Helper()
	old := defaultConfig
	defaultConfig = filepath.Join(t.TempDir(), "config.shcl")
	t.Cleanup(func() { defaultConfig = old })
}

// noHostChecks leaves repair with only the safe settings and plugin steps.
func noHostChecks(t *testing.T) {
	t.Helper()
	old := repairSteps
	repairSteps = func(inv invocation) (*repairpkg.Host, []repairpkg.Check, []repairpkg.Check) {
		return &repairpkg.Host{Cfg: &inv.cfg}, nil, nil
	}
	t.Cleanup(func() { repairSteps = old })
}

// fakePaperMC points paperAPI at a stand-in that has build 249 of 26.3 only.
func fakePaperMC(t *testing.T) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/paper":
			fmt.Fprint(w, `{"versions":{"26.3":["26.3"]}}`)
		case "/paper/versions/26.3/builds":
			fmt.Fprintf(w, `[{"id":249,"channel":"STABLE","downloads":{"server:default":{"name":"paper-26.3-249.jar","checksums":{"sha256":"%s"},"size":3,"url":"https://x/jar"}}}]`, strings.Repeat("ab", 32))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := paperAPI
	paperAPI.Base, paperAPI.Client = srv.URL+"/paper", srv.Client()
	t.Cleanup(func() { paperAPI = old })
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
		{"repair", "--approve-plugins", "--check"},
		{"status", "--set", "heapMax"},
		{"status", "--set", "=6G"},
		{"status", "--set", "noSuchKey=1"},
		{"status", "--set", "runMode=lxc"},
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
		{[]string{"start"}, "podman", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"start", "--native"}, "native", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"start", "--native", "--set", "runMode=docker"}, "docker", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"start", "--set", "runMode=docker", "--native"}, "native", "minecraftd", "minecraftd", 4 << 30},
		{[]string{"--set", "heapMax=6G", "start", "--set", "runUser=mc"}, "podman", "mc", "mc", 6 << 30},
		{[]string{"--config", path, "status"}, "podman", "mcfile", "mcfile", 2 << 30},
		{[]string{"status", "--config=" + path, "--set", "heapMax=3G"}, "podman", "mcfile", "mcfile", 3 << 30},
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
	if done || inv.cfg.RunMode != "podman" || !strings.Contains(errOut.String(), "defaults are used") {
		t.Errorf("default missing: done %v, mode %q, stderr %q", done, inv.cfg.RunMode, errOut.String())
	}
	code, _, msg := runArgs("status", "--config", filepath.Join(t.TempDir(), "nope.shcl"))
	if code != exitError || !strings.Contains(msg, "nope.shcl") {
		t.Errorf("--config missing: exit %d, stderr %q; want %d", code, msg, exitError)
	}
	if inv.configPath != defaultConfig {
		t.Errorf("configPath = %q, want the default", inv.configPath)
	}
}

// watch redraws until Ctrl-C, and sends no screen codes to a file or pipe.
func TestWatch(t *testing.T) {
	defer testid.Line(t, "df5yg")
	cfg := config.Defaults()
	cfg.ServerDir, cfg.OwnerTrees = t.TempDir(), nil
	probe := status.NewProbe(&host.Fake{}, &cfg, filepath.Join(t.TempDir(), "plugins.shcl"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	if err := watch(ctx, probe, &out, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "Every 10ms, until Ctrl-C."); n < 2 || strings.Contains(out.String(), "\x1b") {
		t.Errorf("%d redraws, or a screen code:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "Server:     not running in podman mode\n") {
		t.Errorf("no status in:\n%s", out.String())
	}
}

// repair fixes the safe settings as root, and --check only reports, without
// root. Either one exits 1 while something is wrong.
func TestRepair(t *testing.T) {
	defer testid.Line(t, "df5ys")
	noDefaultConfig(t)
	noHostChecks(t)
	dir := t.TempDir()
	set := "--set=serverDir=" + dir
	code, _, msg := runArgs("repair", set)
	if os.Geteuid() != 0 && (code != exitError || !strings.Contains(msg, "needs root")) {
		t.Errorf("not root: exit %d, stderr %q", code, msg)
	}
	code, out, _ := runArgs("repair", "--check", set)
	if code != exitError || !strings.Contains(out, "FAILED  server.properties: white-list isn't set") || !strings.Contains(out, "warning: the whitelist is empty") {
		t.Errorf("--check: exit %d, stdout %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "server.properties")); err == nil {
		t.Error("--check wrote server.properties")
	}

	inv, _, done := prepare([]string{"repair", set}, &bytes.Buffer{}, &bytes.Buffer{})
	if done {
		t.Fatal("prepare stopped")
	}
	var b bytes.Buffer
	if err := repair(context.Background(), inv, strings.NewReader(""), &b, true); err != nil || !strings.Contains(b.String(), "FIXED   server.properties: white-list is now \"true\"") {
		t.Errorf("as root: %v, %q", err, b.String())
	}
	code, out, _ = runArgs("repair", "--check", set)
	if code != exitOK || !strings.HasPrefix(out, "OK      safe settings\n") {
		t.Errorf("after fix: exit %d, stdout %q", code, out)
	}
}

// repair reports jars that aren't approved, and --approve-plugins asks about
// each one: yes approves it, no moves it out, and no answer leaves it.
func TestRepairApprovePlugins(t *testing.T) {
	defer testid.Line(t, "df5z5")
	noDefaultConfig(t)
	noHostChecks(t)
	dir := t.TempDir()
	for name, text := range map[string]string{"plugins/a.jar": "a", "plugins/b.jar": "b", "plugins/c.jar": "c", "paper.jar": "p"} {
		if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	set := "--set=serverDir=" + dir
	code, out, _ := runArgs("repair", "--check", set)
	if code != exitError || !strings.Contains(out, "FAILED  plugins/a.jar isn't approved; repair --approve-plugins") || !strings.Contains(out, "FAILED  paper.jar isn't approved") {
		t.Errorf("--check: exit %d, stdout %q", code, out)
	}

	repairWith := func(answers string) (string, error) {
		t.Helper()
		inv, _, done := prepare([]string{"repair", "--approve-plugins", set}, &bytes.Buffer{}, &bytes.Buffer{})
		if done {
			t.Fatal("prepare stopped")
		}
		var b bytes.Buffer
		err := repair(context.Background(), inv, strings.NewReader(answers), &b, true)
		return b.String(), err
	}
	out, err := repairWith("y\nmaybe\nn\ny\n")
	for _, want := range []string{
		"plugins/a.jar (new)\n  sha256: ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb\n  size:   1 bytes\n",
		"  says:   no plugin.yml\nApprove it? [y/n] \n",
		"Approve it? [y/n] Approve it? [y/n] ",
		"FIXED   plugins/b.jar refused, and moved to " + filepath.Join(dir, "plugins-refused", "b.jar"),
		"FIXED   plugins/a.jar approved\nFIXED   plugins/c.jar approved\nFAILED  paper.jar isn't approved; no answer\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("first pass lacks %q:\n%s", want, out)
		}
	}
	if err == nil {
		t.Error("first pass: no error with paper.jar left")
	}

	if out, err := repairWith("y\n"); err != nil || !strings.Contains(out, "paper.jar (new)") || strings.Contains(out, "a.jar") {
		t.Errorf("second pass: %v\n%s", err, out)
	}
	code, out, _ = runArgs("repair", "--check", set)
	if code != exitOK || !strings.Contains(out, "OK      plugins: 3 jars, all approved\n") {
		t.Errorf("after: exit %d, stdout %q", code, out)
	}
}

// cmd takes the rest of the line as the console command; console takes none.
func TestConsoleCmdArgs(t *testing.T) {
	defer testid.Line(t, "df5z8")
	noDefaultConfig(t)
	var out, errOut bytes.Buffer
	inv, _, done := prepare([]string{"cmd", "--set", "heapMax=2G", "whitelist", "add", "Steve"}, &out, &errOut)
	if done || inv.opts.line != "whitelist add Steve" || inv.cfg.HeapMax != 2<<30 {
		t.Errorf("cmd: done %v, line %q, heapMax %d, stderr %q", done, inv.opts.line, inv.cfg.HeapMax, errOut.String())
	}
	for _, args := range [][]string{{"cmd"}, {"console", "now"}, {"console", "--dry-run"}} {
		if code, _, errOut := runArgs(args...); code != exitUsage || errOut == "" {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
	}
	if code, out, _ := runArgs("cmd", "--help"); code != exitOK || !strings.Contains(out, "tt-minecraftd cmd [options] <server command>") {
		t.Errorf("cmd --help: exit %d, output %q", code, out)
	}
	if code, out, _ := runArgs("--help"); code != exitOK || !strings.Contains(out, "console") || !strings.Contains(out, "cmd ") {
		t.Errorf("--help: exit %d, output %q", code, out)
	}
}

// repair runs the host checks before the safe settings and after the plugins,
// and a failed one makes the exit 1.
func TestRepairOrder(t *testing.T) {
	defer testid.Line(t, "df5zn")
	noDefaultConfig(t)
	old := repairSteps
	t.Cleanup(func() { repairSteps = old })
	repairSteps = func(inv invocation) (*repairpkg.Host, []repairpkg.Check, []repairpkg.Check) {
		early := repairpkg.Check{Name: "early", Look: func(context.Context, *repairpkg.Host) ([]repairpkg.Finding, error) { return nil, nil }}
		late := repairpkg.Check{Name: "late", Look: func(context.Context, *repairpkg.Host) ([]repairpkg.Finding, error) {
			return []repairpkg.Finding{{Problem: "late is wrong"}}, nil
		}}
		return &repairpkg.Host{Cfg: &inv.cfg}, []repairpkg.Check{early}, []repairpkg.Check{late}
	}
	code, out, _ := runArgs("repair", "--check", "--set=serverDir="+t.TempDir())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != exitError || lines[0] != "OK      early" || !strings.HasPrefix(lines[1], "FAILED  server.properties: online-mode") || lines[len(lines)-1] != "FAILED  late is wrong" {
		t.Errorf("exit %d, stdout:\n%s", code, out)
	}
}

// setup runs itself through sudo unless it's root or a dry run, and on an
// existing install it runs repair.
func TestSetup(t *testing.T) {
	defer testid.Line(t, "df65m")
	noDefaultConfig(t)
	noHostChecks(t)
	fakePaperMC(t)
	var again []string
	old := asRoot
	asRoot = func(args []string) error { again = args; return nil }
	t.Cleanup(func() { asRoot = old })

	code, out, _ := runArgs("setup", "--help")
	for _, want := range []string{"--yes ", "--dry-run ", "--adopt PATH", "--accept-eula ", "--native "} {
		if code != exitOK || !strings.Contains(out, want) {
			t.Errorf("setup --help lacks %q: %q", want, out)
		}
	}
	if os.Geteuid() != 0 {
		if code, _, msg := runArgs("setup", "--yes", "--adopt", "/srv/x"); code != exitOK || !reflect.DeepEqual(again, []string{"setup", "--yes", "--adopt", "/srv/x"}) {
			t.Errorf("not root: exit %d, ran again with %q, stderr %q", code, again, msg)
		}
	}

	dir := t.TempDir()
	conf := filepath.Join(dir, "config.shcl")
	if err := os.WriteFile(conf, []byte("serverDir: "+dir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, msg := runArgs("setup", "--dry-run", "--config", conf)
	if code != exitError || !strings.Contains(out, "Found an install at "+conf) || !strings.Contains(out, "FAILED  server.properties: white-list isn't set") {
		t.Errorf("existing install: exit %d, stdout %q, stderr %q", code, out, msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "server.properties")); err == nil {
		t.Error("a dry run wrote server.properties")
	}
}

// update runs itself through sudo unless it's root or a dry run, and a dry
// run shows the plan without downloading.
func TestUpdate(t *testing.T) {
	defer testid.Line(t, "df671")
	noDefaultConfig(t)
	fakePaperMC(t)
	var again []string
	old := asRoot
	asRoot = func(args []string) error { again = args; return nil }
	t.Cleanup(func() { asRoot = old })

	code, out, _ := runArgs("update", "--help")
	for _, want := range []string{"--paper ", "--plugins ", "--yes ", "--dry-run "} {
		if code != exitOK || !strings.Contains(out, want) {
			t.Errorf("update --help lacks %q: %q", want, out)
		}
	}
	if os.Geteuid() != 0 {
		if code, _, msg := runArgs("update", "--paper", "--yes"); code != exitOK || !reflect.DeepEqual(again, []string{"update", "--paper", "--yes"}) {
			t.Errorf("not root: exit %d, ran again with %q, stderr %q", code, again, msg)
		}
	}

	dir := t.TempDir()
	if err := os.Symlink("paper-26.3-244.jar", filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "paper-26.3-244.jar"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, msg := runArgs("update", "--dry-run", "--set=serverDir="+dir)
	if code != exitOK || !strings.Contains(out, "from build 244 to build 249") || !strings.Contains(out, "would download paper-26.3-249.jar") {
		t.Errorf("dry run: exit %d, stdout %q, stderr %q", code, out, msg)
	}
	if target, _ := os.Readlink(filepath.Join(dir, "paper.jar")); target != "paper-26.3-244.jar" {
		t.Errorf("a dry run moved paper.jar to %q", target)
	}
}
