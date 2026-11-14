// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package setup

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/repair"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// rig is one setup run on a temp folder. The fake runner makes the folder
// that install -d names, as the real one would.
type rig struct {
	s       *Setup
	f       *host.Fake
	out     *bytes.Buffer
	dir     string
	server  string
	ps      string
	repairs []bool
	repCfg  config.Config
	paper   fakePaper
	geyser  fakeGeyser
	// nft is whether nftables is installed.
	nft bool
}

// fakePaper hands out one build, and writes it as a file.
type fakePaper struct {
	err       error
	installed []string
}

func (p *fakePaper) NewestVersion(context.Context) (string, error) { return "26.3", p.err }

func (p *fakePaper) NewestStable(_ context.Context, v string) (paper.Build, error) {
	return paper.Build{Version: v, ID: 249, Name: "paper-26.3-249.jar", SHA256: strings.Repeat("ab", 32), Size: 3}, nil
}

func (p *fakePaper) Install(_ context.Context, dir string, b paper.Build) error {
	p.installed = append(p.installed, b.Name)
	if err := os.WriteFile(filepath.Join(dir, b.Name), []byte("jar"), 0o600); err != nil {
		return err
	}
	return os.Symlink(b.Name, filepath.Join(dir, "paper.jar"))
}

// fakeGeyser hands out one build of each plugin, and writes it in plugins/.
type fakeGeyser struct {
	err       error
	asked     []string
	installed []string
}

func (g *fakeGeyser) Latest(_ context.Context, p string) (geyser.Build, error) {
	g.asked = append(g.asked, p)
	name := map[string]string{"geyser": "Geyser-Spigot.jar", "floodgate": "floodgate-spigot.jar"}[p]
	return geyser.Build{Project: p, Version: "2.0", ID: 7, Name: name, SHA256: strings.Repeat("cd", 32)}, g.err
}

func (g *fakeGeyser) Install(_ context.Context, dir string, b geyser.Build) error {
	g.installed = append(g.installed, b.Name)
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o750); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, b.Path()), []byte("jar"), 0o600)
}

func (g *fakeGeyser) Source(p string) string { return "https://example.com/" + p }

func newRig(t *testing.T, tools []string, answers string, sets ...string) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{out: &bytes.Buffer{}, dir: dir, server: filepath.Join(dir, "srv", "server")}
	passwd := filepath.Join(dir, "passwd")
	group := filepath.Join(dir, "group")
	resolv := filepath.Join(dir, "resolv.conf")
	r.nft = true
	for file, text := range map[string]string{passwd: "alice:x:1000:1000::/home/alice:/bin/bash\n", group: "alice:x:1000:\n", resolv: "nameserver 192.168.1.1\n"} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.f = &host.Fake{Reply: func(c host.Cmd) ([]byte, error) {
		switch {
		case c.Args[0] == "ps":
			return []byte(r.ps), nil
		case c.Args[0] == "apt-get":
			r.nft = r.nft || slices.Contains(c.Args, "nftables")
		case c.Args[0] == "install" && c.Args[1] == "-d":
			return nil, os.MkdirAll(c.Args[len(c.Args)-1], 0o750)
		case c.Args[0] == "groupadd":
			return nil, appendLine(group, c.Args[len(c.Args)-1]+":x:990:")
		case c.Args[0] == "useradd":
			return nil, appendLine(passwd, c.Args[len(c.Args)-1]+":x:990:990::/nonexistent:/usr/sbin/nologin")
		case c.Args[0] == "gpasswd":
			b, err := os.ReadFile(group) // #nosec G304 -- the test's own file
			if err != nil {
				return nil, err
			}
			text := strings.Replace(string(b), c.Args[3]+":x:990:", c.Args[3]+":x:990:"+c.Args[2], 1)
			return nil, os.WriteFile(group, []byte(text), 0o600)
		}
		return nil, nil
	}}
	opts := []config.Option{{Key: "serverDir", Value: r.server}, {Key: "ownerAfterStop", Value: "alice"}}
	for _, kv := range sets {
		k, v, _ := strings.Cut(kv, "=")
		opts = append(opts, config.Option{Key: k, Value: v})
	}
	r.s = &Setup{
		Run:         r.f,
		ConfigPath:  filepath.Join(dir, "etc", "config.shcl"),
		PluginsFile: filepath.Join(dir, "etc", "plugins.shcl"),
		Sets:        opts,
		In:          bufio.NewScanner(strings.NewReader(answers)),
		Out:         r.out,
		Exe:         filepath.Join(dir, "tt-minecraftd"),
		Accounts: func(c *config.Config) *repair.Host {
			return &repair.Host{Run: r.f, Cfg: c, Passwd: passwd, Group: group,
				RulesFile: filepath.Join(dir, "firewall.nft"), UnitFile: filepath.Join(dir, "fw.service"), ResolvConf: resolv, BootFile: filepath.Join(dir, "boot.service")}
		},
		Paper:  &r.paper,
		Geyser: &r.geyser,
		Repair: func(_ context.Context, c *config.Config, check bool) error {
			r.repairs = append(r.repairs, check)
			r.repCfg = *c
			return nil
		},
	}
	was, wasCat, wasNft := launch.Installed, hasCatatonit, hasNft
	launch.Installed = func(name string) bool { return slices.Contains(tools, name) }
	hasCatatonit = func() bool { return slices.Contains(tools, "catatonit") }
	hasNft = func() bool { return r.nft }
	t.Cleanup(func() { launch.Installed, hasCatatonit, hasNft = was, wasCat, wasNft })
	return r
}

func appendLine(file, line string) error {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- the test's own file
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (r *rig) ran() []string { return r.f.Lines() }

func hasAll(t *testing.T, what string, have []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.ContainsFunc(have, func(s string) bool { return strings.Contains(s, w) }) {
			t.Errorf("%s lacks %q:\n%s", what, w, strings.Join(have, "\n"))
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- a test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNewInstall(t *testing.T) {
	defer testid.Line(t, "df65d")
	r := newRig(t, []string{"podman", "catatonit"}, "y\ny\n")
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	hasAll(t, "commands", r.ran(),
		"sudo groupadd --system minecraftd",
		"sudo useradd --system --gid minecraftd",
		"sudo gpasswd --add alice minecraftd",
		"sudo install -d -o alice -g minecraftd -m 0770 "+r.server,
		"sudo podman pull docker.io/library/eclipse-temurin:",
		"sudo install -m 0755 "+r.s.Exe+" "+BinPath,
	)
	if slices.ContainsFunc(r.ran(), func(s string) bool { return strings.Contains(s, "apt-get") }) {
		t.Errorf("apt-get ran with podman and catatonit there:\n%s", strings.Join(r.ran(), "\n"))
	}
	if !reflect.DeepEqual(r.paper.installed, []string{"paper-26.3-249.jar"}) {
		t.Errorf("Paper installs: %q", r.paper.installed)
	}
	approved, _, err := plugins.ReadApproved(r.s.PluginsFile)
	if err != nil || len(approved) != 3 || approved[0].Name != "paper-26.3-249.jar" || approved[0].SHA256 != strings.Repeat("ab", 32) {
		t.Errorf("approved list: %v %v", approved, err)
	}
	if !reflect.DeepEqual(r.geyser.installed, []string{"Geyser-Spigot.jar", "floodgate-spigot.jar"}) {
		t.Errorf("Geyser installs: %q", r.geyser.installed)
	}
	hasAll(t, "output", strings.Split(r.out.String(), "\n"), "setup will:", "Pull docker.io/", "Download Paper 26.3 build 249", "FIXED   Paper 26.3 build 249 downloaded", "eula.txt: eula is now true", "server.properties: white-list is now", "Download Geyser 2.0 build 7 and Floodgate 2.0 build 7 into plugins/", "FIXED   plugins/floodgate-spigot.jar approved")
	if !reflect.DeepEqual(r.repairs, []bool{true}) {
		t.Errorf("repair runs: %v, want one --check", r.repairs)
	}
	// The file setup wrote loads back to what it checked, with no warnings.
	cfg, warns, err := config.Load(r.s.ConfigPath, nil)
	if err != nil || len(warns) > 0 {
		t.Fatalf("written config: %v %v\n%s", err, warns, readFile(t, r.s.ConfigPath))
	}
	if !reflect.DeepEqual(cfg, r.repCfg) {
		t.Errorf("written config loads as\n%+v\nnot\n%+v", cfg, r.repCfg)
	}
	if cfg.RunMode != "podman" {
		t.Errorf("runMode %q", cfg.RunMode)
	}
	if fi, err := os.Stat(r.s.ConfigPath); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("config mode: %v %v", fi.Mode(), err)
	}
	if !strings.Contains(readFile(t, filepath.Join(r.server, "eula.txt")), "eula=true") {
		t.Error("eula.txt doesn't say eula=true")
	}
}

func TestRuntimePick(t *testing.T) {
	defer testid.Line(t, "df65e")
	// A no to Podman falls back to native mode only while it's on.
	noWant, noErr := "native", ""
	if !config.NativeMode {
		noWant, noErr = "", "needs Podman or Docker"
	}
	for _, c := range []struct {
		name    string
		tools   []string
		answers string
		sets    []string
		ask     bool
		want    string
		apt     string
		err     string
	}{
		{name: "podman", tools: []string{"podman", "docker", "catatonit"}, want: "podman"},
		{name: "docker only", tools: []string{"docker"}, want: "docker"},
		{name: "podman, no catatonit", tools: []string{"podman", "apt-get"}, want: "podman", apt: "apt-get install -y catatonit"},
		{name: "neither, yes", tools: []string{"apt-get"}, answers: "y\ny\n", ask: true, want: "podman", apt: "apt-get install -y podman catatonit"},
		{name: "neither, no", tools: []string{"apt-get"}, answers: "n\ny\n", ask: true, want: noWant, err: noErr},
		{name: "neither, --yes", tools: []string{"apt-get"}, want: "podman", apt: "apt-get install -y podman catatonit"},
		{name: "neither, no apt", err: "neither Podman nor Docker"},
		{name: "given", tools: []string{"podman"}, sets: []string{"runMode=docker"}, want: "docker"},
	} {
		r := newRig(t, c.tools, c.answers, c.sets...)
		r.s.Yes = !c.ask
		r.s.AcceptEULA = true
		err := r.s.Do(context.Background())
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: error %v, want %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v\n%s", c.name, err, r.out)
			continue
		}
		if r.repCfg.RunMode != c.want {
			t.Errorf("%s: runMode %q, want %q", c.name, r.repCfg.RunMode, c.want)
		}
		if c.apt != "" {
			hasAll(t, c.name, r.ran(), "sudo "+c.apt)
		}
		pulled := slices.ContainsFunc(r.ran(), func(s string) bool { return strings.Contains(s, " pull ") })
		if pulled != (c.want != "native") {
			t.Errorf("%s: image pulled %v in %s mode", c.name, pulled, c.want)
		}
	}
}

// A fresh image has no package lists, and install alone finds nothing.
func TestAptUpdateFirst(t *testing.T) {
	defer testid.Line(t, "df67t")
	r := newRig(t, []string{"apt-get"}, "")
	r.s.Yes, r.s.AcceptEULA = true, true
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	ran := r.ran()
	upd := slices.Index(ran, "sudo apt-get update")
	inst := slices.IndexFunc(ran, func(s string) bool { return strings.HasPrefix(s, "sudo apt-get install") })
	if upd < 0 || inst != upd+1 {
		t.Errorf("apt-get update at %d, install at %d:\n%s", upd, inst, strings.Join(ran, "\n"))
	}

	r = newRig(t, []string{"podman", "catatonit"}, "")
	r.s.Yes, r.s.AcceptEULA = true, true
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if slices.Contains(r.ran(), "sudo apt-get update") {
		t.Errorf("apt-get update ran with nothing to install")
	}
}

func TestAdopt(t *testing.T) {
	defer testid.Line(t, "df65f")
	r := newRig(t, []string{"docker"}, "")
	r.s.Yes = true
	r.s.AcceptEULA = true
	old := filepath.Join(r.dir, "old", "paper")
	if err := os.MkdirAll(filepath.Join(old, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"paper.jar": "jar", "server.properties": "white-list=false\nmotd=hi\n", "plugins/Thing.jar": "not really a jar", "eula.txt": "eula=true\n"} {
		if err := os.WriteFile(filepath.Join(old, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Without --adopt, a folder with files in it is refused.
	r.s.Sets = append(r.s.Sets, config.Option{Key: "serverDir", Value: old})
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "--adopt "+old) {
		t.Fatalf("new install over files: %v", err)
	}
	if _, err := os.Stat(r.s.ConfigPath); err == nil {
		t.Fatal("config written for a refused setup")
	}

	r.s.Sets = r.s.Sets[:len(r.s.Sets)-1]
	r.s.Adopt = old
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if r.repCfg.ServerDir != old {
		t.Errorf("serverDir %q, want %q", r.repCfg.ServerDir, old)
	}
	hasAll(t, "commands", r.ran(), "sudo chown -R -- alice:minecraftd "+old)
	if slices.ContainsFunc(r.ran(), func(s string) bool { return strings.Contains(s, "install -d") }) {
		t.Error("an adopted tree was made again")
	}
	props := readFile(t, filepath.Join(old, "server.properties"))
	if !strings.Contains(props, "white-list=true") || !strings.Contains(props, "motd=hi") {
		t.Errorf("server.properties:\n%s", props)
	}
	approved, _, err := plugins.ReadApproved(r.s.PluginsFile)
	if err != nil || len(approved) != 4 || !slices.ContainsFunc(approved, func(a plugins.Approved) bool { return a.Name == "paper.jar" }) {
		t.Errorf("approved list, which should have the plugin and paper.jar: %v %v", approved, err)
	}
	if len(r.paper.installed) > 0 {
		t.Errorf("Paper downloaded over an adopted paper.jar: %q", r.paper.installed)
	}
	if strings.Contains(r.out.String(), "EULA") {
		t.Error("asked about the EULA when eula.txt already says true")
	}
}

// An adopted tree's Geyser and Floodgate go on the approved list with
// GeyserMC's source, so update --plugins keeps them current. A jar only
// named like one doesn't, and a changed one keeps the source it had.
func TestAdoptGeyserSource(t *testing.T) {
	defer testid.Line(t, "df76q")
	r := newRig(t, []string{"docker"}, "")
	r.s.Yes = true
	old := filepath.Join(r.dir, "old", "paper")
	if err := os.MkdirAll(filepath.Join(old, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"paper.jar": "jar", "eula.txt": "eula=true\n"} {
		if err := os.WriteFile(filepath.Join(old, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeJar(t, filepath.Join(old, "plugins", "Geyser-Spigot.jar"), "name: Geyser-Spigot\nversion: 2.12.1\n")
	writeJar(t, filepath.Join(old, "plugins", "floodgate-spigot.jar"), "name: floodgate\nversion: 2.2.5\n")
	writeJar(t, filepath.Join(old, "plugins", "geyser-lookalike.jar"), "name: Lookalike\nversion: 1\n")
	if err := os.MkdirAll(filepath.Dir(r.s.PluginsFile), 0o750); err != nil {
		t.Fatal(err)
	}
	own := "https://example.org/own-floodgate"
	if err := plugins.Approve(r.s.PluginsFile, []plugins.Info{{Jar: plugins.Jar{Name: "plugins/floodgate-spigot.jar"}, SHA256: strings.Repeat("cd", 32), Source: own}}); err != nil {
		t.Fatal(err)
	}
	r.s.Adopt = old
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if len(r.geyser.installed) > 0 {
		t.Errorf("downloaded over the adopted jars: %q", r.geyser.installed)
	}
	approved, _, err := plugins.ReadApproved(r.s.PluginsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range approved {
		got[a.Name] = a.Source
	}
	want := map[string]string{
		"paper.jar":                    "",
		"plugins/Geyser-Spigot.jar":    r.geyser.Source("geyser"),
		"plugins/floodgate-spigot.jar": own,
		"plugins/geyser-lookalike.jar": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sources %v, want %v", got, want)
	}
	if !strings.Contains(r.out.String(), "says:   Geyser-Spigot 2.12.1\n  source: "+r.geyser.Source("geyser")+"\n") {
		t.Errorf("plan doesn't show the source:\n%s", r.out)
	}
}

func writeJar(t *testing.T, file, pluginYML string) {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("plugin.yml")
	if err == nil {
		_, err = w.Write([]byte(pluginYML))
	}
	if err == nil {
		err = z.Close()
	}
	if err == nil {
		err = os.WriteFile(file, b.Bytes(), 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// The jars to approve show before Go ahead, so a yes covers jars that were
// seen. A no approves none.
func TestJarsListedBeforeAsking(t *testing.T) {
	defer testid.Line(t, "df67z")
	r := newRig(t, []string{"docker"}, "n\n")
	old := filepath.Join(r.dir, "old", "paper")
	if err := os.MkdirAll(filepath.Join(old, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"paper.jar": "jar", "plugins/Thing.jar": "not really a jar", "eula.txt": "eula=true\n"} {
		if err := os.WriteFile(filepath.Join(old, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.s.Adopt = old
	if err := r.s.Do(context.Background()); err == nil {
		t.Fatal("went on after a no")
	}
	out := r.out.String()
	listed, asked := strings.Index(out, "plugins/Thing.jar\n  sha256: "), strings.Index(out, "Go ahead?")
	if listed < 0 || asked < 0 || listed > asked {
		t.Errorf("jar listed at %d, asked at %d:\n%s", listed, asked, out)
	}
	if _, err := os.Stat(r.s.PluginsFile); err == nil {
		t.Error("approved list written after a no")
	}
}

func TestEULA(t *testing.T) {
	defer testid.Line(t, "df65g")
	for _, c := range []struct {
		name    string
		answers string
		accept  bool
		want    bool
	}{
		{name: "yes", answers: "y\n", want: true},
		{name: "no", answers: "n\n"},
		{name: "no answer, --yes", answers: ""},
		{name: "--accept-eula", accept: true, want: true},
	} {
		r := newRig(t, []string{"docker"}, c.answers)
		r.s.Yes = true
		r.s.AcceptEULA = c.accept
		if err := r.s.Do(context.Background()); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		_, err := os.Stat(filepath.Join(r.server, "eula.txt"))
		if (err == nil) != c.want {
			t.Errorf("%s: eula.txt written %v, want %v", c.name, err == nil, c.want)
		}
		asked := strings.Contains(r.out.String(), "Do you accept the Minecraft EULA?")
		if asked == c.accept {
			t.Errorf("%s: asked %v", c.name, asked)
		}
	}
}

func TestExistingInstall(t *testing.T) {
	defer testid.Line(t, "df65h")
	for _, c := range []struct {
		name    string
		answers string
		dry     bool
		adopt   string
		want    []bool
		err     string
	}{
		{name: "yes", answers: "y\n", want: []bool{false}},
		{name: "no", answers: "n\n", err: "nothing changed"},
		{name: "dry run", dry: true, want: []bool{true}},
		{name: "adopt", adopt: "/srv/other", err: "isn't a new install"},
	} {
		r := newRig(t, []string{"podman"}, c.answers)
		if err := os.MkdirAll(filepath.Dir(r.s.ConfigPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(r.s.ConfigPath, []byte("runMode: podman\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r.s.Loaded = &config.Config{RunMode: "podman"}
		r.s.DryRun = c.dry
		r.s.Adopt = c.adopt
		err := r.s.Do(context.Background())
		if c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: error %v, want %q", c.name, err, c.err)
		}
		if c.err == "" && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(r.repairs, c.want) {
			t.Errorf("%s: repair runs %v, want %v", c.name, r.repairs, c.want)
		}
		if len(r.ran()) > 0 {
			t.Errorf("%s: ran %q", c.name, r.ran())
		}
	}
}

func TestRefusals(t *testing.T) {
	defer testid.Line(t, "df65j")
	r := newRig(t, []string{"podman", "catatonit"}, "")
	r.ps = "safeserver 4242 java -Xmx4G -jar paper-26.3-244.jar --nogui\n"
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "pid 4242") {
		t.Errorf("with a server running: %v", err)
	}
	r.ps = ""

	r.paper.err = errors.New("no network")
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "can't get Paper: no network") || strings.Contains(r.out.String(), "setup will:") {
		t.Errorf("Paper API down: %v", err)
	}
	r.paper.err = nil

	// No answer to the plan is a no.
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "nothing changed") {
		t.Errorf("no answer: %v", err)
	}
	if _, err := os.Stat(r.s.ConfigPath); err == nil {
		t.Error("config written without a yes")
	}

	sets := r.s.Sets
	r.s.Sets = sets[:1]
	t.Setenv("SUDO_USER", "")
	t.Setenv("USER", "")
	account := config.Account
	config.Account = func() string { return "" }
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "who the operator is") {
		t.Errorf("no operator: %v", err)
	}
	config.Account = account
	r.s.Sets = sets

	r.s.Adopt = "/srv/a b"
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "--set serverDir") {
		t.Errorf("adopt with a space: %v", err)
	}
}

func TestDryRun(t *testing.T) {
	defer testid.Line(t, "df65k")
	r := newRig(t, []string{"podman", "catatonit"}, "")
	r.s.DryRun = true
	// The real runner prints and skips these under dry run; the fake would
	// make the folder, so leave it out.
	r.f.Reply = func(host.Cmd) ([]byte, error) { return nil, nil }
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	for _, p := range []string{r.s.ConfigPath, r.server, r.s.PluginsFile} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("dry run made %s", p)
		}
	}
	hasAll(t, "output", strings.Split(r.out.String(), "\n"), "would write "+r.s.ConfigPath, "runMode: podman", "PLAN    group minecraftd made", "PLAN    alice added to group minecraftd", "PLAN    repair --check runs last", "would download paper-26.3-249.jar")
	if len(r.repairs) > 0 {
		t.Errorf("repair runs %v", r.repairs)
	}
}

func TestBedrock(t *testing.T) {
	defer testid.Line(t, "df65z")
	approved := func(r *rig) map[string]plugins.Approved {
		list, _, err := plugins.ReadApproved(r.s.PluginsFile)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]plugins.Approved{}
		for _, a := range list {
			m[a.Name] = a
		}
		return m
	}

	// The sources go in the list, for update --plugins.
	r := newRig(t, []string{"podman", "catatonit"}, "y\ny\n")
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	got := approved(r)
	for name, src := range map[string]string{"plugins/Geyser-Spigot.jar": "https://example.com/geyser", "plugins/floodgate-spigot.jar": "https://example.com/floodgate"} {
		if got[name].Source != src || got[name].SHA256 != strings.Repeat("cd", 32) {
			t.Errorf("%s: %+v", name, got[name])
		}
	}

	// bedrock off asks for neither.
	r = newRig(t, []string{"podman", "catatonit"}, "y\ny\n", "bedrock=false")
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if len(r.geyser.asked) > 0 || strings.Contains(r.out.String(), "Geyser") {
		t.Errorf("bedrock off: asked %q\n%s", r.geyser.asked, r.out)
	}

	// An adopted tree keeps the Geyser it has, and gets only Floodgate.
	r = newRig(t, []string{"podman", "catatonit"}, "")
	r.s.Yes, r.s.AcceptEULA = true, true
	old := filepath.Join(r.dir, "old")
	if err := os.MkdirAll(filepath.Join(old, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"paper.jar": "jar", "plugins/Geyser-Spigot.jar": "mine"} {
		if err := os.WriteFile(filepath.Join(old, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.s.Adopt = old
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if !reflect.DeepEqual(r.geyser.asked, []string{"floodgate"}) || readFile(t, filepath.Join(old, "plugins", "Geyser-Spigot.jar")) != "mine" {
		t.Errorf("adopted: asked %q", r.geyser.asked)
	}
	if got := approved(r); got["plugins/Geyser-Spigot.jar"].Source != "" || got["plugins/floodgate-spigot.jar"].Source == "" {
		t.Errorf("adopted: %+v", got)
	}

	// A failed lookup stops setup before it changes anything.
	r = newRig(t, []string{"podman", "catatonit"}, "y\ny\n")
	r.geyser.err = errors.New("no answer")
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "can't get Geyser: no answer") {
		t.Errorf("failed lookup: %v", err)
	}
	if len(r.ran()) > 1 || strings.Contains(r.out.String(), "setup will:") {
		t.Errorf("a failed lookup went on:\n%s\n%s", strings.Join(r.ran(), "\n"), r.out)
	}
}

// repair only reports a missing Geyser or Floodgate, and says setup gets it.
func TestExistingBedrock(t *testing.T) {
	defer testid.Line(t, "df66b")
	existing := func(answers string, jars ...string) *rig {
		r := newRig(t, nil, answers)
		if err := os.MkdirAll(filepath.Dir(r.s.ConfigPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(r.s.ConfigPath, []byte("runMode: podman\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(r.server, "plugins"), 0o750); err != nil {
			t.Fatal(err)
		}
		for _, j := range jars {
			if err := os.WriteFile(filepath.Join(r.server, "plugins", j), []byte("mine"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		r.s.Loaded = &config.Config{RunMode: "podman", ServerDir: r.server, Bedrock: true}
		return r
	}

	r := existing("y\n", "Geyser-Spigot.jar")
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if !reflect.DeepEqual(r.geyser.installed, []string{"floodgate-spigot.jar"}) || !reflect.DeepEqual(r.repairs, []bool{false}) {
		t.Errorf("Floodgate missing: installed %q, repairs %v\n%s", r.geyser.installed, r.repairs, r.out)
	}
	if !strings.Contains(r.out.String(), "Download Floodgate 2.0 build 7") {
		t.Errorf("no plan:\n%s", r.out)
	}
	list, _, err := plugins.ReadApproved(r.s.PluginsFile)
	if err != nil || len(list) != 1 || list[0].Name != "plugins/floodgate-spigot.jar" || list[0].Source == "" {
		t.Errorf("approved %+v %v", list, err)
	}

	r = existing("n\n")
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "nothing changed") || len(r.geyser.installed) > 0 || len(r.repairs) > 0 {
		t.Errorf("a no went on: %v, installed %q, repairs %v", err, r.geyser.installed, r.repairs)
	}

	r = existing("")
	r.s.DryRun = true
	if err := r.s.Do(context.Background()); err != nil || len(r.geyser.installed) > 0 || !reflect.DeepEqual(r.repairs, []bool{true}) {
		t.Errorf("dry run: %v, installed %q, repairs %v", err, r.geyser.installed, r.repairs)
	}

	r = existing("y\n", "Geyser-Spigot.jar", "floodgate-spigot.jar")
	if err := r.s.Do(context.Background()); err != nil || len(r.geyser.asked) > 0 || !strings.Contains(r.out.String(), "Run repair?") {
		t.Errorf("both there: %v, asked %q\n%s", err, r.geyser.asked, r.out)
	}

	r = existing("y\n")
	r.geyser.err = errors.New("no answer")
	if err := r.s.Do(context.Background()); err == nil || !strings.Contains(err.Error(), "can't get Geyser") || len(r.repairs) > 0 {
		t.Errorf("failed lookup: %v, repairs %v", err, r.repairs)
	}
}

// On an existing install, update runs after repair, even when a repair check
// failed, since update can be what fixes it.
func TestExistingUpdate(t *testing.T) {
	defer testid.Line(t, "df670")
	r := newRig(t, nil, "y\n")
	if err := os.MkdirAll(filepath.Dir(r.s.ConfigPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.s.ConfigPath, []byte("runMode: podman\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.s.Loaded = &config.Config{RunMode: "podman", ServerDir: r.server}
	var order []string
	r.s.Repair = func(context.Context, *config.Config, bool) error {
		order = append(order, "repair")
		return errors.New("no paper.jar")
	}
	r.s.Update = func(_ context.Context, c *config.Config) error {
		order = append(order, "update "+c.ServerDir)
		return nil
	}
	err := r.s.Do(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no paper.jar") || !reflect.DeepEqual(order, []string{"repair", "update " + r.server}) {
		t.Errorf("%v, ran %q\n%s", err, order, r.out)
	}
	if !strings.Contains(r.out.String(), "runs repair and then update on it") || !strings.Contains(r.out.String(), "Run repair and update?") {
		t.Errorf("no plan:\n%s", r.out)
	}

	order = nil
	r.s.In = bufio.NewScanner(strings.NewReader("n\n"))
	if err := r.s.Do(context.Background()); err == nil || len(order) > 0 {
		t.Errorf("a no went on: %v, ran %q", err, order)
	}
}

// Step 9 installs nftables when it's missing, writes the rules and the unit,
// and starts it, before the image pull. Without nft, setup stops there.
func TestFirewall(t *testing.T) {
	defer testid.Line(t, "df66h")
	r := newRig(t, []string{"podman", "catatonit", "apt-get"}, "")
	r.nft = false
	r.s.Yes, r.s.AcceptEULA = true, true
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	rules := filepath.Join(r.dir, "firewall.nft")
	ran := r.ran()
	hasAll(t, "commands", ran,
		"sudo apt-get install -y nftables",
		"sudo install -D -m 0644 -o root -g root -T /dev/stdin "+rules,
		"sudo systemctl enable tt-minecraftd-firewall.service",
		"sudo systemctl reload-or-restart tt-minecraftd-firewall.service",
	)
	loaded := slices.Index(ran, "sudo systemctl reload-or-restart tt-minecraftd-firewall.service")
	pull := slices.IndexFunc(ran, func(s string) bool { return strings.Contains(s, " pull ") })
	if loaded < 0 || pull < loaded {
		t.Errorf("rules loaded at %d, image pulled at %d", loaded, pull)
	}
	hasAll(t, "output", strings.Split(r.out.String(), "\n"), "Write the firewall rules to /etc/tt-minecraftd/firewall.nft", "FIXED   firewall rules loaded")

	r = newRig(t, []string{"podman", "catatonit"}, "")
	r.nft = false
	r.s.Yes, r.s.AcceptEULA = true, true
	err := r.s.Do(context.Background())
	if err == nil || !strings.Contains(r.out.String(), "FAILED  nftables isn't installed") {
		t.Errorf("no nft, no apt: err %v\n%s", err, r.out)
	}
	if _, err := os.Stat(r.s.ConfigPath); err == nil {
		t.Error("config written after the firewall step failed")
	}

	r = newRig(t, []string{"podman", "catatonit", "apt-get"}, "", "firewall=off")
	r.s.Yes, r.s.AcceptEULA = true, true
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if slices.ContainsFunc(r.ran(), func(s string) bool { return strings.Contains(s, "nft") }) || strings.Contains(r.out.String(), "firewall rules") {
		t.Errorf("firewall off:\n%s\n%s", strings.Join(r.ran(), "\n"), r.out)
	}

	r = newRig(t, []string{"podman", "catatonit", "apt-get"}, "")
	r.nft = false
	r.s.DryRun = true
	r.f.Reply = func(host.Cmd) ([]byte, error) { return nil, nil }
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	hasAll(t, "dry run", strings.Split(r.out.String(), "\n"), "PLAN    firewall rules written to /etc/tt-minecraftd/firewall.nft")
}

// Step 11 installs and enables the boot unit with startAtBoot on, after the
// image pull, and doesn't start it. Off, there's no unit.
func TestBootUnit(t *testing.T) {
	defer testid.Line(t, "df78v")
	r := newRig(t, []string{"podman", "catatonit"}, "", "startAtBoot=true")
	r.s.Yes, r.s.AcceptEULA = true, true
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	ran := r.ran()
	unit := filepath.Join(r.dir, "boot.service")
	hasAll(t, "commands", ran, "sudo install -D -m 0644 -o root -g root -T /dev/stdin "+unit, "sudo systemctl enable tt-minecraftd.service")
	enable := slices.Index(ran, "sudo systemctl enable tt-minecraftd.service")
	pull := slices.IndexFunc(ran, func(s string) bool { return strings.Contains(s, " pull ") })
	if pull < 0 || enable < pull || slices.Contains(ran, "sudo systemctl start tt-minecraftd.service") {
		t.Errorf("pulled at %d, unit enabled at %d:\n%s", pull, enable, strings.Join(ran, "\n"))
	}
	hasAll(t, "output", strings.Split(r.out.String(), "\n"), "Install and enable tt-minecraftd.service", "FIXED   tt-minecraftd.service enabled")

	r = newRig(t, []string{"podman", "catatonit"}, "")
	r.s.Yes, r.s.AcceptEULA = true, true
	if err := r.s.Do(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	if strings.Contains(strings.Join(r.ran(), "\n"), "tt-minecraftd.service") || strings.Contains(r.out.String(), "tt-minecraftd.service") {
		t.Errorf("startAtBoot off:\n%s\n%s", strings.Join(r.ran(), "\n"), r.out)
	}
}
