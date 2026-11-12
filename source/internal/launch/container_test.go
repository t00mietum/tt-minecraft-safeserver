// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

type fakeConsole struct{ sent []string }

func (f *fakeConsole) Send(_ context.Context, line string) error {
	f.sent = append(f.sent, line)
	return nil
}

// rig is a server tree with paper.jar linked to a build, one plugin, and a
// fake host whose container state follows the states list, one per look.
type rig struct {
	d       *Boxed
	r       *host.Fake
	console *fakeConsole
	out     *bytes.Buffer
	slept   time.Duration
	dir     string
	ps      string
	states  []string
	log     string
	tables  string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "srv", "server")
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"paper-26.3-244.jar", "plugins/Geyser-Spigot.jar"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("paper-26.3-244.jar", filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.RunMode = "docker"
	cfg.ServerDir, cfg.OwnerTrees = dir, []string{dir}
	cfg.RunGroup, cfg.OwnerAfterStop = "minecraftd", "alice"
	cfg.HeapMin, cfg.MemCap = 1<<30, 6<<30
	cfg.StartTimeout, cfg.StopTimeout = 5*time.Second, 3*time.Second
	if _, err := settings.Fix(&cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whitelist.json"), []byte(`[{"uuid": "5e0c6c7f-4d0a-4d1e-9a0e-4b9e3c2f1a01", "name": "Steve"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Both jars are empty, and approved.
	list := filepath.Join(base, "plugins.shcl")
	empty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if err := os.WriteFile(list, []byte("jar: paper-26.3-244.jar\n\tsha256: "+empty+"\njar: plugins/Geyser-Spigot.jar\n\tsha256: "+empty+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := &rig{r: &host.Fake{}, console: &fakeConsole{}, out: &bytes.Buffer{}, dir: dir, tables: "table inet tt_minecraftd\n"}
	g.r.Reply = g.reply
	noRuntimes(t)
	g.d = &Boxed{
		Kit: Kit{Run: g.r, Console: g.console, Cfg: &cfg, PluginsFile: list, Out: g.out, Sleep: func(d time.Duration) { g.slept += d }},
		IDs: func(string, string) (string, string, error) { return "990", "991", nil },
	}
	return g
}

// noRuntimes has no runtime installed but runMode's, unless a test adds one.
func noRuntimes(t *testing.T) {
	was := Installed
	Installed = func(string) bool { return false }
	t.Cleanup(func() { Installed = was })
}

func (g *rig) reply(c host.Cmd) ([]byte, error) {
	switch {
	case c.Args[0] == "ps":
		return []byte(g.ps), nil
	case len(c.Args) > 2 && c.Args[1] == "ps":
		if len(g.states) == 0 {
			return nil, nil
		}
		s := g.states[0]
		if len(g.states) > 1 {
			g.states = g.states[1:]
		}
		return []byte(s + "\n"), nil
	case len(c.Args) > 2 && c.Args[1] == "logs":
		return []byte(g.log), nil
	case c.Args[0] == "nft":
		return []byte(g.tables), nil
	}
	return nil, nil
}

// changes are the commands that aren't looks, as they'd print.
func (g *rig) changes() []string {
	var lines []string
	for i, c := range g.r.Ran {
		if !c.Look {
			lines = append(lines, g.r.Lines()[i])
		}
	}
	return lines
}

func TestDockerStart(t *testing.T) {
	defer testid.Line(t, "df5xp")
	g := newRig(t)
	g.states = []string{"exited", "running"}
	g.log = "[12:00:09 INFO]: Done (8.1s)! For help, type \"help\"\n"
	if err := g.d.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return host.Quote([]string{s}) }
	cfg := g.d.Cfg
	run := []string{"docker", "run", "--detach", "--name", "tt-minecraftd", "--label", "tt-minecraftd",
		"--user", "990:991", "--network", "host", "--read-only", "--tmpfs", "/tmp:rw,exec,nosuid,nodev",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "2048",
		"--memory", "6442450944", "--memory-swap", "6442450944", "--oom-score-adj", "1000",
		"--init", "--interactive", "--tty", "--workdir", g.dir,
		"--mount", "type=bind,src=" + g.dir + ",dst=" + g.dir,
		"--mount", "type=bind,src=" + g.dir + "/paper-26.3-244.jar,dst=" + g.dir + "/paper-26.3-244.jar,readonly",
		"--mount", "type=bind,src=" + g.dir + "/plugins/Geyser-Spigot.jar,dst=" + g.dir + "/plugins/Geyser-Spigot.jar,readonly",
		cfg.Image, "sh", "-c", `umask 0007 && exec "$@"`, "sh", "java", "-Xms1G", "-Xmx4G"}
	run = append(run, cfg.JvmArgs...)
	run = append(run, "-Dlog4j2.formatMsgNoLookups=true", "-jar", "paper.jar", "--nogui")
	want := []string{
		"sudo -v",
		"sudo docker rm tt-minecraftd",
		"sudo chown -R -- minecraftd:minecraftd " + q(g.dir),
		"sudo chmod -R ug+rwX,o-rwx -- " + q(g.dir),
		"sudo " + host.Quote(run),
	}
	if got := g.changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(g.out.String(), "The server is up.") {
		t.Errorf("output %q", g.out.String())
	}
}

// Another Paper server, such as one the old scripts started, stops start
// before it changes anything.
func TestDockerStartRefuses(t *testing.T) {
	defer testid.Line(t, "df5xq")
	g := newRig(t)
	g.ps = "safeserver 4242 java -Xmx6G -jar paper-26.3-244.jar nogui\n"
	err := g.d.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "pid 4242, as safeserver") || len(g.r.Ran) != 1 {
		t.Errorf("err %v, ran %q; want refused after ps only", err, g.r.Lines())
	}

	g = newRig(t)
	g.states = []string{"running"}
	if err := g.d.Start(context.Background(), StartMode{}); err == nil || len(g.changes()) != 1 {
		t.Errorf("container running: err %v, ran %q", err, g.changes())
	}

	g = newRig(t)
	if err := os.Remove(filepath.Join(g.dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if err := g.d.Start(context.Background(), StartMode{}); err == nil || !strings.Contains(err.Error(), "no paper.jar") {
		t.Errorf("no paper.jar: err %v", err)
	}
}

// A server that dies while loading gets its log tail shown, and one that's
// slow is left running with an error.
func TestDockerStartFails(t *testing.T) {
	defer testid.Line(t, "df5xr")
	g := newRig(t)
	g.states = []string{"", "exited"}
	g.log = "Error: Unable to access jarfile paper.jar\n"
	err := g.d.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "exited while loading") || !strings.Contains(g.out.String(), "Unable to access jarfile") {
		t.Errorf("err %v, output %q", err, g.out.String())
	}

	g = newRig(t)
	g.states = []string{"", "running"}
	err = g.d.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "didn't finish loading") || g.slept != 5*time.Second {
		t.Errorf("slow: err %v, slept %s", err, g.slept)
	}
}

// --debug runs attached and hands the trees back after. --foreground skips
// sudo and waits for the container, for the boot unit. Dry run waits for nothing.
func TestDockerStartModes(t *testing.T) {
	defer testid.Line(t, "df5xs")
	g := newRig(t)
	if err := g.d.Start(context.Background(), StartMode{Debug: true}); err != nil {
		t.Fatal(err)
	}
	got := g.changes()
	if len(got) != 5 || strings.Contains(got[3], "--detach") || !g.r.Ran[len(g.r.Ran)-2].Tty || !strings.Contains(got[4], "chown -R -- alice:minecraftd") {
		t.Errorf("debug ran %q", got)
	}

	g = newRig(t)
	g.d.Root = true
	g.states = []string{"", "running"}
	g.log = "Done (1.0s)! For help, type \"help\""
	if err := g.d.Start(context.Background(), StartMode{Foreground: true}); err != nil {
		t.Fatal(err)
	}
	lines := g.r.Lines()
	if lines[len(lines)-2] != "sudo docker wait tt-minecraftd" || strings.Contains(strings.Join(lines, "\n"), "sudo -v") {
		t.Errorf("foreground ran %q", lines)
	}

	g = newRig(t)
	g.d.DryRun = true
	if err := g.d.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	if last := g.r.Lines()[len(g.r.Ran)-1]; !strings.HasPrefix(last, "sudo docker run --detach") || g.slept != 0 {
		t.Errorf("dry run ended with %q, slept %s", last, g.slept)
	}
}

func TestDockerStop(t *testing.T) {
	defer testid.Line(t, "df5xt")
	g := newRig(t)
	g.states = []string{"running", "running", "running", "exited"}
	if err := g.d.Stop(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	wantSent := []string{
		"say The server stops in 12 seconds.", "say The server stops in 10 seconds.", "say The server stops in 5 seconds.",
		"say The server stops in 4 seconds.", "say The server stops in 3 seconds.", "say The server stops in 2 seconds.",
		"say The server stops in 1 second.", "stop",
	}
	if !reflect.DeepEqual(g.console.sent, wantSent) {
		t.Errorf("sent %q", g.console.sent)
	}
	if g.slept != 13*time.Second {
		t.Errorf("slept %s, want 12s of countdown and 1s of waiting", g.slept)
	}
	want := []string{"sudo -v", "sudo chown -R -- alice:minecraftd " + host.Quote([]string{g.dir})}
	if got := g.changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
}

// Not running: the trees still go back, as after a crash. Stuck: SIGTERM,
// then SIGKILL after docker's grace time.
func TestDockerStopEdges(t *testing.T) {
	defer testid.Line(t, "df5xv")
	g := newRig(t)
	g.states = []string{"exited"}
	if err := g.d.Stop(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	if len(g.console.sent) != 0 || len(g.changes()) != 2 || !strings.Contains(g.changes()[1], "chown") {
		t.Errorf("not running: sent %q, ran %q", g.console.sent, g.changes())
	}

	g = newRig(t)
	g.states = []string{"running"}
	if err := g.d.Stop(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	got := g.changes()
	if len(got) != 3 || got[1] != "sudo docker stop --time 30 tt-minecraftd" || g.slept != 3*time.Second {
		t.Errorf("stuck: ran %q, slept %s", got, g.slept)
	}

	g = newRig(t)
	g.states = []string{"running"}
	g.d.DryRun = true
	if err := g.d.Stop(context.Background(), 90); err != nil {
		t.Fatal(err)
	}
	if g.slept != 0 || len(g.console.sent) != 10 {
		t.Errorf("dry run: slept %s, sent %q", g.slept, g.console.sent)
	}

	g = newRig(t)
	g.states = []string{"running"}
	g.d.Console = failConsole{}
	if err := g.d.Stop(context.Background(), 0); err == nil || len(g.changes()) != 1 {
		t.Errorf("console down: err %v, ran %q; want no hand-back while it runs", err, g.changes())
	}
}

type failConsole struct{}

func (failConsole) Send(context.Context, string) error { return errors.New("no socket") }

// An unsafe setting stops a start in either mode, after sudo and before
// anything changes. A likely mistake only warns.
func TestStartChecksSettings(t *testing.T) {
	defer testid.Line(t, "df5yj")
	for _, native := range []bool{false, true} {
		g := newNativeRig(t)
		start := g.d.Start
		if native {
			start = g.n.Start
		}
		props := filepath.Join(g.dir, "server.properties")
		text, err := os.ReadFile(props)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(props, []byte(strings.Replace(string(text), "white-list=true", "white-list=false", 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		err = start(context.Background(), StartMode{})
		if err == nil || !strings.Contains(err.Error(), "repair") || len(g.changes()) != 1 || !strings.Contains(g.out.String(), "white-list is \"false\"") {
			t.Errorf("native %v: err %v, ran %q, output %q", native, err, g.changes(), g.out.String())
		}

		g = newNativeRig(t)
		start = g.d.Start
		if native {
			start = g.n.Start
		}
		g.n.DryRun, g.d.DryRun = true, true
		if err := os.WriteFile(filepath.Join(g.dir, "whitelist.json"), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := start(context.Background(), StartMode{}); err != nil && strings.Contains(err.Error(), "safe settings") {
			t.Errorf("native %v, empty whitelist: %v", native, err)
		}
		if !strings.Contains(g.out.String(), "warning: the whitelist is empty") {
			t.Errorf("native %v, empty whitelist: output %q", native, g.out.String())
		}
	}
}

// With firewall on, a start without the table stops after sudo, in either
// mode, before anything changes. With it off, nft isn't asked.
func TestStartChecksFirewall(t *testing.T) {
	defer testid.Line(t, "df66f")
	for _, native := range []bool{false, true} {
		g := newNativeRig(t)
		start := g.d.Start
		if native {
			start = g.n.Start
		}
		g.tables = "table inet filter\n"
		err := start(context.Background(), StartMode{})
		if err == nil || !strings.Contains(err.Error(), "inet tt_minecraftd") || !strings.Contains(err.Error(), "repair") || len(g.changes()) != 1 {
			t.Errorf("native %v, no table: err %v, ran %q", native, err, g.changes())
		}
		var asked bool
		for _, line := range g.r.Lines() {
			asked = asked || line == "sudo nft list tables inet"
		}
		if !asked {
			t.Errorf("native %v: ran %q, no nft list", native, g.r.Lines())
		}

		g = newNativeRig(t)
		start = g.d.Start
		if native {
			start = g.n.Start
		}
		g.tables = ""
		g.d.Cfg.Firewall = false
		_ = start(context.Background(), StartMode{}) // only what ran matters here
		for _, c := range g.r.Ran {
			if c.Args[0] == "nft" {
				t.Errorf("native %v, firewall off: asked nft", native)
			}
		}
	}
}

// A new or changed jar stops a start in either mode, after sudo and before
// anything changes. Geyser extensions count, and get read-only mounts too.
func TestStartChecksPlugins(t *testing.T) {
	defer testid.Line(t, "df5z4")
	empty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	ext := filepath.Join("plugins", "Geyser-Spigot", "extensions", "thing.jar")
	for _, native := range []bool{false, true} {
		for _, c := range []struct {
			what, file, text, says string
		}{
			{"new", "plugins/floodgate.jar", "", "not approved: plugins/floodgate.jar"},
			{"changed", "plugins/Geyser-Spigot.jar", "evil", "changed since it was approved: plugins/Geyser-Spigot.jar"},
			{"paper", "paper-26.3-244.jar", "evil", "changed since it was approved: paper-26.3-244.jar"},
			{"extension", ext, "", "not approved: " + filepath.ToSlash(ext)},
		} {
			g := newNativeRig(t)
			start := g.d.Start
			if native {
				start = g.n.Start
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(g.dir, c.file)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(g.dir, c.file), []byte(c.text), 0o600); err != nil {
				t.Fatal(err)
			}
			err := start(context.Background(), StartMode{})
			if err == nil || !strings.Contains(err.Error(), "repair --approve-plugins") || len(g.changes()) != 1 || !strings.Contains(g.out.String(), c.says) {
				t.Errorf("native %v, %s: err %v, ran %q, output %q", native, c.what, err, g.changes(), g.out.String())
			}
		}

		g := newNativeRig(t)
		start := g.d.Start
		if native {
			start = g.n.Start
		}
		g.d.PluginsFile, g.n.PluginsFile = filepath.Join(t.TempDir(), "none.shcl"), filepath.Join(t.TempDir(), "none.shcl")
		if err := start(context.Background(), StartMode{}); err == nil || !strings.Contains(g.out.String(), "not approved: paper-26.3-244.jar") {
			t.Errorf("native %v, no list: err %v, output %q", native, err, g.out.String())
		}
	}

	g := newRig(t)
	g.d.DryRun = true
	if err := os.MkdirAll(filepath.Join(g.dir, filepath.Dir(ext)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, ext), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(g.d.PluginsFile, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("jar: " + filepath.ToSlash(ext) + "\n\tsha256: " + empty + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := g.d.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	mount := "type=bind,src=" + filepath.Join(g.dir, ext) + ",dst=" + filepath.Join(g.dir, ext) + ",readonly"
	if last := g.r.Lines()[len(g.r.Ran)-1]; !strings.Contains(last, mount) {
		t.Errorf("no read-only mount for the extension in %q", last)
	}
}

// Podman gets the same run command as Docker, through sudo.
func TestPodmanStart(t *testing.T) {
	defer testid.Line(t, "df64f")
	docker := newRig(t)
	docker.states = []string{"", "running"}
	docker.log = "Done (1.0s)! For help, type \"help\""
	if err := docker.d.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	podman := newRig(t)
	podman.d.Cfg.RunMode = "podman"
	podman.d.Cfg.ServerDir, podman.d.Cfg.OwnerTrees = docker.d.Cfg.ServerDir, docker.d.Cfg.OwnerTrees
	podman.states, podman.log = []string{"", "running"}, docker.log
	if err := podman.d.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	if len(podman.r.Ran) != len(docker.r.Ran) {
		t.Fatalf("podman ran %q\ndocker ran %q", podman.r.Lines(), docker.r.Lines())
	}
	boxed := 0
	for i, c := range podman.r.Ran {
		d := docker.r.Ran[i]
		if d.Args[0] != "docker" {
			if !reflect.DeepEqual(c, d) {
				t.Errorf("podman ran %q, docker %q", podman.r.Lines()[i], docker.r.Lines()[i])
			}
			continue
		}
		boxed++
		if c.Args[0] != "podman" || !c.Sudo || !d.Sudo || !reflect.DeepEqual(c.Args[1:], d.Args[1:]) {
			t.Errorf("podman ran %q, docker %q", podman.r.Lines()[i], docker.r.Lines()[i])
		}
	}
	if boxed < 3 {
		t.Errorf("only %d runtime commands: %q", boxed, podman.r.Lines())
	}
}

// A container from before a runMode change is found under the other runtime,
// and stop goes there. Only runMode's runtime has to answer.
func TestFindContainer(t *testing.T) {
	defer testid.Line(t, "df64g")
	states := map[string]string{}
	fails := map[string]bool{}
	installed := map[string]bool{}
	r := &host.Fake{Reply: func(c host.Cmd) ([]byte, error) {
		if fails[c.Args[0]] {
			return nil, errors.New(c.Args[0] + ": can't connect")
		}
		if c.Args[1] == "ps" {
			return []byte(states[c.Args[0]] + "\n"), nil
		}
		return nil, nil
	}}
	was := Installed
	Installed = func(name string) bool { return installed[name] }
	t.Cleanup(func() { Installed = was })
	find := func() string {
		rt, state, err := FindContainer(context.Background(), r, "podman")
		if err != nil {
			return "error"
		}
		return rt + " " + state
	}
	for _, c := range []struct {
		podman, docker string
		dockerIn       bool
		fail           string
		want           string
	}{
		{"", "", false, "", " "},
		{"running", "", false, "", "podman running"},
		{"", "exited", false, "", " "},
		{"", "exited", true, "", "docker exited"},
		{"exited", "running", true, "", "docker running"},
		{"exited", "exited", true, "", "podman exited"},
		{"exited", "", true, "", "podman exited"},
		{"", "", true, "docker", " "},
		{"", "", true, "podman", "error"},
	} {
		states["podman"], states["docker"], installed["docker"] = c.podman, c.docker, c.dockerIn
		fails = map[string]bool{c.fail: true}
		if got := find(); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
	for _, c := range r.Ran {
		if !c.Sudo || !c.Look {
			t.Errorf("not a sudo look: %q", c.Args)
		}
	}

	g := newRig(t)
	g.d.Cfg.RunMode = "podman"
	installed["docker"] = true
	Installed = func(name string) bool { return installed[name] }
	left := 2
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		if c.Args[0] == "docker" && c.Args[1] == "ps" && left > 0 {
			left--
			return []byte("running\n"), nil
		}
		if c.Args[0] == "docker" && c.Args[1] == "ps" {
			return []byte("exited\n"), nil
		}
		return g.reply(c)
	}
	if err := g.d.Stop(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if g.d.Runtime != "docker" || !strings.Contains(g.out.String(), "under docker, not podman") {
		t.Errorf("stop stayed on %q: %q", g.d.Runtime, g.out.String())
	}
	for _, c := range g.r.Ran[2:] {
		if c.Args[0] == "podman" {
			t.Errorf("asked podman after finding docker's: %q", g.r.Lines())
			break
		}
	}
}
