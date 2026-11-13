// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	repairpkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/repair"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/selfupdate"
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

// noHostRunner gives setup and update a stand-in runner, and setup's account
// checks stand-in passwd and group files with minecraftd in them. PATH is
// empty and launch.Installed false, so nothing on the host can run. The count
// goes up each time setup or update takes the runner.
func noHostRunner(t *testing.T) (*host.Fake, *int) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	fake, took := &host.Fake{}, new(int)
	oldRunner, oldSteps, oldInstalled := hostRunner, setupSteps, launch.Installed
	t.Cleanup(func() { hostRunner, setupSteps, launch.Installed = oldRunner, oldSteps, oldInstalled })
	hostRunner = func(bool) host.Runner { *took++; return fake }
	launch.Installed = func(string) bool { return false }
	etc := t.TempDir()
	passwd, group := filepath.Join(etc, "passwd"), filepath.Join(etc, "group")
	if err := os.WriteFile(passwd, []byte("minecraftd:x:990:991::/nonexistent:/usr/sbin/nologin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte("minecraftd:x:991:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setupSteps = func(inv invocation) (host.Runner, func(c *config.Config) *repairpkg.Host) {
		*took++
		return fake, func(c *config.Config) *repairpkg.Host {
			return &repairpkg.Host{Run: fake, Cfg: c, ConfigPath: inv.configPath, Passwd: passwd, Group: group,
				RulesFile: filepath.Join(etc, "rules.nft"), UnitFile: filepath.Join(etc, "unit"), ResolvConf: filepath.Join(etc, "resolv.conf"), BootFile: filepath.Join(etc, "boot.service"),
				NftThere: func() bool { return false }}
		}
	}
	return fake, took
}

// fakePaperMC points paperAPI at a stand-in that has build 249 of 26.3 only.
func fakePaperMC(t *testing.T) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/paper":
			fmt.Fprint(w, `{"versions":{"26.3":["26.3"]}}`)
		case "/paper/versions/26.3":
			fmt.Fprint(w, `{"version":{"id":"26.3","java":{"version":{"minimum":25}}}}`)
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

// fakeReleases points selfAPI at a stand-in GitHub with release v9.0.0, and
// counts the lookups.
func fakeReleases(t *testing.T) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bin := selfupdate.AssetName(runtime.GOARCH)
		switch r.URL.Path {
		case "/repo/releases":
			hits.Add(1)
			fmt.Fprintf(w, `[{"tag_name":"v9.0.0","assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"SHA256SUMS","browser_download_url":"%s/sums"}]}]`, bin, srv.URL, srv.URL)
		case "/sums":
			fmt.Fprintf(w, "%s  %s\n", strings.Repeat("ab", 32), bin)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := selfAPI
	selfAPI.Base, selfAPI.Client = srv.URL+"/repo", srv.Client()
	t.Cleanup(func() { selfAPI = old })
	return &hits
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
		{[]string{"--version"}, []string{"tt-minecraftd " + version + " build "}},
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
		{"repair", "--approve-plugins", "--dry-run"},
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

// repair fixes the safe settings as root, running itself through sudo when
// it isn't, and --check or --dry-run only reports, without root. Either one
// exits 1 while something is wrong.
func TestRepair(t *testing.T) {
	defer testid.Line(t, "df5ys")
	noDefaultConfig(t)
	noHostChecks(t)
	var again []string
	old := asRoot
	asRoot = func(args []string) error { again = args; return nil }
	t.Cleanup(func() { asRoot = old })
	dir := t.TempDir()
	set := "--set=serverDir=" + dir
	if os.Geteuid() != 0 {
		if code, _, msg := runArgs("repair", set); code != exitOK || !reflect.DeepEqual(again, []string{"repair", set}) {
			t.Errorf("not root: exit %d, ran again with %q, stderr %q", code, again, msg)
		}
	}
	for _, flag := range []string{"--check", "--dry-run"} {
		again = nil
		code, out, _ := runArgs("repair", flag, set)
		if code != exitError || again != nil || !strings.Contains(out, "FAILED  server.properties: white-list isn't set") || !strings.Contains(out, "warning: the whitelist is empty") {
			t.Errorf("%s: exit %d, ran again with %q, stdout %q", flag, code, again, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "server.properties")); err == nil {
			t.Errorf("%s wrote server.properties", flag)
		}
	}

	inv, _, done := prepare([]string{"repair", set}, &bytes.Buffer{}, &bytes.Buffer{})
	if done {
		t.Fatal("prepare stopped")
	}
	var b bytes.Buffer
	if err := repair(context.Background(), inv, strings.NewReader(""), &b, true); err != nil || !strings.Contains(b.String(), "FIXED   server.properties: white-list is now \"true\"") {
		t.Errorf("as root: %v, %q", err, b.String())
	}
	code, out, _ := runArgs("repair", "--check", set)
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

// A Geyser jar approved through repair gets GeyserMC's source, as setup's do.
func TestRepairApproveGeyserSource(t *testing.T) {
	defer testid.Line(t, "df76r")
	noDefaultConfig(t)
	noHostChecks(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("plugin.yml")
	if err == nil {
		_, err = w.Write([]byte("name: Geyser-Spigot\nversion: 2.12.1\n"))
	}
	if err == nil {
		err = z.Close()
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "plugins", "Geyser-Spigot.jar"), b.Bytes(), 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	inv, _, done := prepare([]string{"repair", "--approve-plugins", "--set=serverDir=" + dir}, &bytes.Buffer{}, &bytes.Buffer{})
	if done {
		t.Fatal("prepare stopped")
	}
	var out bytes.Buffer
	_ = repair(context.Background(), inv, strings.NewReader("y\n"), &out, true) // paper.jar is missing, which isn't this test's
	src := geyserAPI.Source("geyser")
	if !strings.Contains(out.String(), "  source: "+src+"\nApprove it?") {
		t.Errorf("source not shown:\n%s", out.String())
	}
	approved, _, err := plugins.ReadApproved(inv.pluginsFile())
	if err != nil || len(approved) != 1 || approved[0].Source != src {
		t.Errorf("approved %v, %v; want the source %s", approved, err, src)
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

// stop, console, and cmd follow a server that runs natively while runMode
// names a container, and the other way round. Podman to Docker is Boxed's.
func TestFollowMode(t *testing.T) {
	defer testid.Line(t, "df67x")
	old := serverMode
	t.Cleanup(func() { serverMode = old })
	for _, c := range []struct{ runMode, running, want, say string }{
		{"podman", "native", "native", "runs in native mode, though runMode says podman"},
		{"native", "podman", "podman", "runs in podman mode, though runMode says native"},
		{"docker", "podman", "docker", ""},
		{"native", "native", "native", ""},
		{"podman", "", "podman", ""},
	} {
		serverMode = func(context.Context, *config.Config) string { return c.running }
		cfg := config.Defaults()
		cfg.RunMode = c.runMode
		var out bytes.Buffer
		follow(context.Background(), &cfg, &out)
		if cfg.RunMode != c.want || (c.say == "") != (out.Len() == 0) || !strings.Contains(out.String(), c.say) {
			t.Errorf("%s with the server %q: runMode %s, said %q", c.runMode, c.running, cfg.RunMode, out.String())
		}
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
	hits := fakeReleases(t)
	_, took := noHostRunner(t)
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
	if !strings.Contains(out, "Updating:") || hits.Load() != 0 {
		t.Errorf("setup's update looked for a release %d times:\n%s", hits.Load(), out)
	}
	// setup, and its own update.
	if *took != 2 {
		t.Errorf("setup took the stand-in runner %d times, not 2", *took)
	}
}

// update runs itself through sudo unless it's root or a dry run, and a dry
// run shows the plan without downloading.
func TestUpdate(t *testing.T) {
	defer testid.Line(t, "df671")
	noDefaultConfig(t)
	fakePaperMC(t)
	hits := fakeReleases(t)
	_, took := noHostRunner(t)
	var again []string
	old := asRoot
	asRoot = func(args []string) error { again = args; return nil }
	t.Cleanup(func() { asRoot = old })

	code, out, _ := runArgs("update", "--help")
	for _, want := range []string{"--self ", "--paper ", "--plugins ", "--image ", "--yes ", "--dry-run "} {
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
	*took = 0 // the not-root run above took it too
	code, out, msg := runArgs("update", "--dry-run", "--set=serverDir="+dir)
	if code != exitOK || !strings.Contains(out, "from build 244 to build 249") || !strings.Contains(out, "would download paper-26.3-249.jar") || !strings.Contains(out, "would pull docker.io/library/eclipse-temurin:25-jre,") {
		t.Errorf("dry run: exit %d, stdout %q, stderr %q", code, out, msg)
	}
	if target, _ := os.Readlink(filepath.Join(dir, "paper.jar")); target != "paper-26.3-244.jar" {
		t.Errorf("a dry run moved paper.jar to %q", target)
	}
	if !strings.Contains(out, "Move tt-minecraftd from "+version+" to v9.0.0") || !strings.Contains(out, "would download tt-minecraftd v9.0.0 and rename it over /") || hits.Load() != 1 {
		t.Errorf("dry run, %d lookups: stdout %q", hits.Load(), out)
	}

	// Only --self needs no server folder.
	code, out, msg = runArgs("update", "--self", "--dry-run", "--set=serverDir="+filepath.Join(dir, "none"))
	if code != exitOK || !strings.Contains(out, "to v9.0.0") || strings.Contains(out, "Paper") {
		t.Errorf("--self: exit %d, stdout %q, stderr %q", code, out, msg)
	}
	if *took != 2 {
		t.Errorf("update took the stand-in runner %d times, not 2", *took)
	}
}

// --mc-version is in the help, and one that can't work is refused before
// sudo asks for a password.
func TestUpdateMCVersion(t *testing.T) {
	defer testid.Line(t, "df78n")
	noDefaultConfig(t)
	var again []string
	old := asRoot
	asRoot = func(args []string) error { again = args; return nil }
	t.Cleanup(func() { asRoot = old })

	if code, out, _ := runArgs("update", "--help"); code != exitOK || !strings.Contains(out, "--mc-version V") {
		t.Errorf("update --help: %q", out)
	}
	for _, args := range [][]string{{"update", "--image", "--mc-version", "26.4"}, {"update", "--mc-version", "26.4;x"}} {
		if code, _, msg := runArgs(args...); code == exitOK || again != nil || !strings.Contains(msg, "--mc-version") {
			t.Errorf("%q: exit %d, ran again with %q, stderr %q", args, code, again, msg)
		}
	}
	if os.Geteuid() != 0 {
		args := []string{"update", "--mc-version", "26.4", "--yes"}
		if code, _, msg := runArgs(args...); code != exitOK || !reflect.DeepEqual(again, args) {
			t.Errorf("not root: exit %d, ran again with %q, stderr %q", code, again, msg)
		}
	}
}

// With agent set, update asks it as the operator, hands the answer to the
// run through sudo in a file only the operator can read, and removes it
// after. The flag that names the file isn't in the help.
func TestUpdateAgent(t *testing.T) {
	defer testid.Line(t, "df76m")
	if os.Geteuid() == 0 {
		t.Skip("the agent doesn't run as root")
	}
	noDefaultConfig(t)
	_, took := noHostRunner(t)
	dir := t.TempDir()
	server := filepath.Join(dir, "server")
	if err := os.MkdirAll(filepath.Join(server, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(server, "plugins", "Thing.jar"), []byte("thing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := plugins.Approve(filepath.Join(dir, "plugins.shcl"), []plugins.Info{{Jar: plugins.Jar{Name: "plugins/Thing.jar"}, SHA256: strings.Repeat("ab", 32)}}); err != nil {
		t.Fatal(err)
	}
	ask := filepath.Join(dir, "ask")
	if err := os.WriteFile(ask, []byte("#!/bin/sh\nwhile read -r _; do :; done\necho '{\"jars\": []}'\n"), 0o700); err != nil { // #nosec G306 -- the test's own agent
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.shcl")
	if err := os.WriteFile(cfg, []byte("serverDir: "+server+"\nagent: command\nagentCommand: ["+ask+"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var again []string
	var handed os.FileInfo
	var answer []byte
	old := asRootAndWait
	asRootAndWait = func(args []string) error {
		again = args
		file := strings.TrimPrefix(args[len(args)-1], "--agent-answer=")
		handed, _ = os.Stat(file)
		answer, _ = os.ReadFile(file) // #nosec G304 -- the test's own temp file
		return exitCode(3)
	}
	t.Cleanup(func() { asRootAndWait = old })

	code, out, msg := runArgs("update", "--plugins", "--config", cfg)
	if code != 3 || msg != "" || len(again) != 5 || !strings.HasPrefix(again[4], "--agent-answer=") || !strings.Contains(out, "Asking the update agent about 1 jar.") {
		t.Fatalf("exit %d, ran again with %q\nstdout %q\nstderr %q", code, again, out, msg)
	}
	if handed == nil || handed.Mode().Perm() != 0o600 || !strings.Contains(string(answer), `"jar":"plugins/Thing.jar"`) {
		t.Errorf("handed %v: %s", handed, answer)
	}
	file := strings.TrimPrefix(again[4], "--agent-answer=")
	if _, err := os.Stat(filepath.Dir(file)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the answer's folder is still there: %v", err)
	}

	// The run through sudo reads it; a dry run stands in for root here.
	file = filepath.Join(dir, "answer.json")
	if err := os.WriteFile(file, answer, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, msg = runArgs("update", "--plugins", "--dry-run", "--config", cfg, "--agent-answer="+file)
	if code != exitOK || !strings.Contains(out, "plugins/Thing.jar: no answer from the update agent") {
		t.Errorf("replay: exit %d\nstdout %q\nstderr %q", code, out, msg)
	}

	if *took != 2 {
		t.Errorf("update took the stand-in runner %d times, not 2", *took)
	}

	if _, out, _ := runArgs("update", "--help"); strings.Contains(out, "agent-answer") {
		t.Errorf("help shows the hand-off flag: %q", out)
	}
}

func TestBuildNumber(t *testing.T) {
	defer testid.Line(t, "df785")
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		// The example in the CICD rules: 2026-10-24 22:00 UTC.
		{time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), "decp8"},
		{time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), "0"},
		{time.Date(2000, 1, 1, 0, 31, 59, 0, time.UTC), "z"},
		{time.Date(2000, 1, 1, 0, 32, 0, 0, time.UTC), "10"},
	} {
		if got := buildNumber(c.at); got != c.want {
			t.Errorf("buildNumber(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

func TestVersionLine(t *testing.T) {
	defer testid.Line(t, "df786")
	at := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)
	date := at.Local().Format("2006-01-02")
	if got, want := versionLine(at, true, false), "tt-minecraftd "+version+" build decp8 ("+date+")"; got != want {
		t.Errorf("a non-release build: %q, want %q", got, want)
	}
	if got, want := versionLine(at, true, true), "tt-minecraftd "+version+" build decp8"; got != want {
		t.Errorf("a release build: %q, want %q", got, want)
	}
	if got, want := versionLine(time.Time{}, false, false), "tt-minecraftd "+version+" build unknown"; got != want {
		t.Errorf("no commit time: %q, want %q", got, want)
	}
}

// With the boot unit enabled for the config, start can't take --debug or
// --foreground, since systemctl runs it, and the unit is asked about that
// config.
func TestBootUnitRefuses(t *testing.T) {
	defer testid.Line(t, "df78w")
	noDefaultConfig(t)
	old := bootServes
	t.Cleanup(func() { bootServes = old })
	var asked []string
	bootServes = func(_ context.Context, path string) (bool, error) {
		asked = append(asked, path)
		return true, nil
	}
	for _, flag := range []string{"--debug", "--foreground"} {
		code, _, errOut := runArgs("start", flag, "--set=serverDir="+t.TempDir())
		if code != exitError || !strings.Contains(errOut, "the boot unit tt-minecraftd.service is enabled") {
			t.Errorf("start %s: exit %d, %q", flag, code, errOut)
		}
	}
	if len(asked) != 2 || asked[0] != defaultConfig {
		t.Errorf("asked about %q, want %s", asked, defaultConfig)
	}
}
