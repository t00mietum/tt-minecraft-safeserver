// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package status

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

var cid = strings.Repeat("d8f5", 16)

// boxedPs is the process table with the server in a container: the
// container's init, then the server under it, 2h 12m old.
func boxedPs(init string) string {
	return "    0 4241 4200 7921 " + init + ` -- sh -c umask 0007 && exec "$@" sh java -Xms2G -Xmx2G -jar paper.jar --nogui
    0 4242 4241 7920 java -Xms2G -Xmx2G -Dlog4j2.formatMsgNoLookups=true -jar paper.jar --nogui
 1001 5000    1  300 bash
`
}

const ssOut = `tcp   LISTEN 0 50  *:25565 *:*
tcp   ESTAB  0 0   [::ffff:192.168.1.9]:25565 [::ffff:192.168.1.20]:51000
tcp   ESTAB  0 0   [::ffff:192.168.1.9]:25565 [::ffff:203.0.113.7]:40000
tcp   ESTAB  0 0   192.168.1.9:22 192.168.1.20:50000
tcp   LISTEN 0 50  127.0.0.1:125565 *:*
tcp   LISTEN 0 4096 *:19132 *:*
udp   UNCONN 0 0   0.0.0.0:19132 0.0.0.0:*
`

// rig is a server tree, an approved list, and a fake host.
type rig struct {
	p       *Probe
	r       *host.Fake
	dir     string
	replies map[string]string
	files   map[string]string
	pinged  string
}

func write(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sum(text string) string {
	s := sha256.Sum256([]byte(text))
	return hex.EncodeToString(s[:])
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "srv", "server")
	write(t, filepath.Join(dir, "paper-26.3-244.jar"), "")
	if err := os.Symlink("paper-26.3-244.jar", filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "plugins", "Geyser-Spigot.jar"), "geyser")
	write(t, filepath.Join(dir, "plugins", "floodgate.jar"), "floodgate")
	write(t, filepath.Join(dir, "whitelist.json"), `[{"uuid":"1","name":"Steve"},{"uuid":"2","name":"Alex"},{"uuid":"3","name":".Bedrock1"}]`)
	write(t, filepath.Join(dir, "ops.json"), `[{"uuid":"1","name":"Steve","level":4},{"uuid":"9","name":"Evil\u001b[2J"}]`)
	var log strings.Builder
	for i := 1; i <= 15; i++ {
		fmt.Fprintf(&log, "[12:00:%02d INFO]: line %d\n", i, i)
	}
	write(t, filepath.Join(dir, "logs", "latest.log"), log.String())
	list := filepath.Join(t.TempDir(), "plugins.shcl")
	write(t, list, "jar: plugins/Geyser-Spigot.jar\n\tsha256: "+sum("geyser")+"\njar: plugins/floodgate.jar\n\tsha256: "+sum("floodgate")+"\njar: paper-26.3-244.jar\n\tsha256: "+sum("")+"\n")

	cfg := config.Defaults()
	cfg.RunMode = "docker"
	cfg.ServerDir, cfg.OwnerTrees = dir, []string{dir}
	cfg.MemCap = 4 << 30
	g := &rig{r: &host.Fake{}, dir: dir, replies: map[string]string{}}
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		if !c.Look {
			t.Errorf("status ran a change: %q", c.Args)
		}
		if c.Sudo || c.As != "" {
			t.Errorf("status used sudo: %q", c.Args)
		}
		key := c.Args[0] + " " + c.Args[1]
		if c.Args[0] == "systemctl" {
			key += " " + c.Args[len(c.Args)-1]
		}
		return []byte(g.replies[key]), nil
	}
	g.files = map[string]string{}
	g.cgroup("4242", "/system.slice/docker-"+cid+".scope", 1<<30)
	g.p = &Probe{
		Run: g.r, Cfg: &cfg, PluginsFile: list,
		ReadFile: func(f string) ([]byte, error) {
			if s, ok := g.files[f]; ok {
				return []byte(s), nil
			}
			return nil, fs.ErrNotExist
		},
		Ping: func(_ context.Context, addr string) (server.PingReply, error) {
			g.pinged = addr
			var r server.PingReply
			r.Version.Name = "Paper 26.3"
			r.Players.Online, r.Players.Max = 2, 20
			r.Players.Sample = append(r.Players.Sample, struct {
				Name string `json:"name"`
			}{"Steve"})
			return r, nil
		},
	}
	g.replies["ss -H"] = ssOut
	g.replies["systemctl show tt-minecraftd.service"] = "enabled\n"
	return g
}

// cgroup puts a process in a cgroup v2 path, with memory in use.
func (g *rig) cgroup(pid, path string, memory int64) {
	g.files["/proc/"+pid+"/cgroup"] = "0::" + path + "\n"
	g.files["/sys/fs/cgroup"+path+"/memory.current"] = fmt.Sprint(memory) + "\n"
}

func (g *rig) text(t *testing.T, width int) string {
	t.Helper()
	var b strings.Builder
	if err := g.p.Take(context.Background()).Write(&b, width); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func wantLines(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w+"\n") {
			t.Errorf("output lacks line %q:\n%s", w, out)
		}
	}
}

func TestStatusDocker(t *testing.T) {
	defer testid.Line(t, "df5y7")
	g := newRig(t)
	g.replies["ps -eo"] = boxedPs("/sbin/docker-init")
	out := g.text(t, 0)
	wantLines(t, out,
		"Server:     running in docker mode, as root, for 2h 12m",
		"Version:    Paper 26.3 (paper-26.3-244.jar)",
		"Players:    2 of 20 online: Steve",
		"Memory:     1.0 GiB of 4.0 GiB cap (25%)",
		"Ports:      tcp *:25565 listening, 2 connections",
		"            tcp *:19132 listening, 0 connections",
		"            udp 0.0.0.0:19132 listening",
		"Firewall:   on; run with sudo to see the drop counters",
		"Whitelist:  3 players",
		"Ops:        Steve, Evil[2J",
		"Plugins:    3 jars, all approved",
		"Boot unit:  enabled",
		"Log:        "+filepath.Join(g.dir, "logs", "latest.log"),
		"            [12:00:06 INFO]: line 6",
		"            [12:00:15 INFO]: line 15",
	)
	if strings.Contains(out, "line 5\n") {
		t.Errorf("log shows more than %d lines:\n%s", logLines, out)
	}
	if !strings.HasPrefix(out, "\nServer:") || !strings.HasSuffix(out, "\n\n") || !strings.Contains(out, "cap (25%)\n\nPorts:") {
		t.Errorf("want blank lines at the start, the end, and between sections:\n%s", out)
	}
	if g.pinged != "127.0.0.1:25565" {
		t.Errorf("pinged %s", g.pinged)
	}
}

func TestStatusNative(t *testing.T) {
	defer testid.Line(t, "df5y8")
	g := newRig(t)
	g.p.Cfg.RunMode = "native"
	g.p.Cfg.BindIP = "192.168.1.9"
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	g.replies["ps -eo"] = fmt.Sprintf("%s 7000 1 180000 /usr/bin/java -Xmx2G -jar paper.jar --nogui\n", me.Uid)
	g.cgroup("7000", "/system.slice/tt-minecraftd-server.service", 512<<20)
	out := g.text(t, 0)
	wantLines(t, out,
		"Server:     running in native mode, as "+me.Username+", for 2d 2h",
		"Memory:     512 MiB of 4.0 GiB cap (12%)",
	)
	if g.pinged != "192.168.1.9:25565" {
		t.Errorf("pinged %s, want bindIP", g.pinged)
	}
}

// Mode gives the server's mode from its cgroup, whatever runMode says.
func TestProbeMode(t *testing.T) {
	defer testid.Line(t, "df67y")
	g := newRig(t)
	if mode, err := g.p.Mode(context.Background()); mode != "" || err != nil {
		t.Errorf("no server: %q, %v", mode, err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	g.replies["ps -eo"] = fmt.Sprintf("%s 7000 1 60 /usr/bin/java -Xmx2G -jar paper.jar --nogui\n", me.Uid)
	g.cgroup("7000", "/system.slice/tt-minecraftd-server.service", 512<<20)
	if mode, err := g.p.Mode(context.Background()); mode != "native" || err != nil {
		t.Errorf("native server under runMode %s: %q, %v", g.p.Cfg.RunMode, mode, err)
	}
}

// As root, status shows the drop counters, and leaves out the ones the
// config can't bump.
func TestStatusFirewall(t *testing.T) {
	defer testid.Line(t, "df67g")
	g := newRig(t)
	g.replies["ps -eo"] = boxedPs("/sbin/docker-init")
	g.p.Root = true
	counter := func(table, name string, n int) string {
		return fmt.Sprintf(`{"counter": {"family": "inet", "name": %q, "table": %q, "handle": 2, "packets": %d, "bytes": %d}}`, name, table, n, n*60)
	}
	list := func(c ...string) string {
		return `{"nftables": [{"metainfo": {"version": "1.1.6", "json_schema_version": 1}}` + strings.Join(append([]string{""}, c...), ", ") + "]}"
	}
	g.replies["nft -j"] = list(
		counter("tt_minecraftd", "in_refused", 31), counter("tt_minecraftd", "in_flood", 2), counter("tt_minecraftd", "in_closed", 7),
		counter("tt_minecraftd", "out_local", 0), counter("tt_minecraftd", "out_lan", 4), counter("tt_minecraftd", "out_web", 1),
		counter("filter", "out_lan", 999),
	)
	wantLines(t, g.text(t, 0),
		"Firewall:   dropped in: 31 not in allowFrom, 2 over joinRateLimit, 7 to closed ports",
		"            dropped out: 0 to loopback, 4 to the LAN, 1 elsewhere",
	)

	g.p.Cfg.AllowFrom, g.p.Cfg.Egress = []string{"any"}, "open"
	g.replies["nft -j"] = list(counter("tt_minecraftd", "in_flood", 2), counter("tt_minecraftd", "out_local", 3))
	wantLines(t, g.text(t, 0),
		"Firewall:   dropped in: 2 over joinRateLimit, no in_closed counter",
		"            dropped out: 3 to loopback, no out_lan counter",
	)

	g.replies["nft -j"] = list(counter("filter", "in_flood", 2))
	wantLines(t, g.text(t, 0), "Firewall:   table inet tt_minecraftd isn't loaded, so start refuses; repair loads it")

	g.replies["nft -j"] = "Error: oops"
	if out := g.text(t, 0); !strings.Contains(out, "Firewall:   can't read nft's counter list") {
		t.Errorf("a bad reply isn't named:\n%s", out)
	}
}

// Rootful Podman, which status can't ask without sudo. crun puts the server in
// a cgroup under the container's scope.
func TestStatusPodman(t *testing.T) {
	defer testid.Line(t, "df64j")
	g := newRig(t)
	g.p.Cfg.RunMode = "podman"
	g.replies["podman ps"] = "" // as without sudo
	g.replies["ps -eo"] = boxedPs("/run/podman-init")
	g.cgroup("4242", "/machine.slice/libpod-"+cid+".scope/container", 1<<30)
	wantLines(t, g.text(t, 0), "Server:     running in podman mode, as root, for 2h 12m", "Memory:     1.0 GiB of 4.0 GiB cap (25%)")
	g.p.Cfg.RunMode = "docker"
	wantLines(t, g.text(t, 0), "Server:     running in podman mode, as root, for 2h 12m; runMode says docker")
}

// Only a Paper server in a container that this tool started counts as the
// server. Any other one is named, since start refuses to run beside it.
func TestStatusOtherPaper(t *testing.T) {
	defer testid.Line(t, "df652")
	g := newRig(t)
	g.replies["ps -eo"] = "    0 3001 3000 60 /sbin/docker-init -- /start\n    0 3002 3001 60 java -jar paper.jar\n" + boxedPs("/sbin/docker-init")
	g.cgroup("3002", "/docker/"+cid, 1<<20)
	g.cgroup("4242", "/user.slice/libpod-conmon-"+cid+".scope", 1<<30)
	wantLines(t, g.text(t, 0),
		"Server:     not running in docker mode",
		"            another Paper server is running: pid 3002, as root",
		"            another Paper server is running: pid 4242, as root",
	)
	g.cgroup("4242", "/docker/"+cid, 1<<30)
	out := g.text(t, 0)
	wantLines(t, out, "Server:     running in docker mode, as root, for 2h 12m", "            another Paper server is running: pid 3002, as root")
	if strings.Contains(out, "pid 4242") {
		t.Errorf("the server named as another one:\n%s", out)
	}
	for cg, want := range map[string]string{
		"0::/system.slice/docker-" + cid + ".scope\n":                    "docker",
		"0::/docker.slice/docker-batch.slice/docker-" + cid + ".scope\n": "docker",
		"12:memory:/docker/" + cid + "\n1:name=systemd:/docker/" + cid:   "docker",
		"0::/machine.slice/libpod-" + cid + ".scope\n":                   "podman",
		"0::/machine.slice/libpod-conmon-" + cid + ".scope\n":            "",
		"0::/system.slice/tt-minecraftd-server.service\n":                "native",
		"0::/system.slice/tt-minecraftd.service\n":                       "",
		"0::/user.slice/user-1001.slice/session-2.scope\n":               "",
		"0::/docker-" + cid[:12] + ".scope\n":                            "",
	} {
		if got := cgroupMode([]byte(cg)); got != want {
			t.Errorf("cgroupMode(%q) = %q, want %q", cg, got, want)
		}
	}
}

// Stopped, with an old script's server still up, a changed jar, and the
// firewall and Bedrock off.
func TestStatusStopped(t *testing.T) {
	defer testid.Line(t, "df5y9")
	g := newRig(t)
	g.replies["ps -eo"] = "    0 811 1 99999 /usr/bin/java -Xmx4G -jar paper-26.3-244.jar --nogui\n"
	g.replies["systemctl show tt-minecraftd.service"] = ""
	g.p.Cfg.Firewall, g.p.Cfg.Bedrock = false, false
	write(t, filepath.Join(g.dir, "plugins", "floodgate.jar"), "swapped")
	write(t, filepath.Join(g.dir, "plugins", "new.jar"), "new")
	if err := os.Remove(filepath.Join(g.dir, "ops.json")); err != nil {
		t.Fatal(err)
	}
	out := g.text(t, 0)
	wantLines(t, out,
		"Server:     not running in docker mode",
		"            another Paper server is running: pid 811, as root",
		"Version:    paper-26.3-244.jar",
		"Memory:     none in use; 4.0 GiB cap",
		"Ports:      tcp *:25565 listening, 2 connections",
		"Firewall:   off in the config",
		"Ops:        none",
		"Plugins:    4 jars, NOT all approved, so start refuses; new: plugins/new.jar; changed: plugins/floodgate.jar",
		"Boot unit:  not installed",
	)
	if g.pinged != "" || strings.Contains(out, "udp") || strings.Contains(out, "Players:") {
		t.Errorf("pinged %q, or showed udp or players while stopped:\n%s", g.pinged, out)
	}
}

// A part that can't be read says why, and the rest still shows.
func TestStatusBroken(t *testing.T) {
	defer testid.Line(t, "df5ya")
	g := newRig(t)
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		return nil, errors.New(c.Args[0] + ": permission denied")
	}
	g.p.PluginsFile = filepath.Join(t.TempDir(), "plugins.shcl")
	write(t, filepath.Join(g.dir, "whitelist.json"), "{not json")
	out := g.text(t, 30)
	wantLines(t, out,
		"Server:     can't list process",
		"Ports:      can't list sockets",
		"Whitelist:  can't read it: whi",
		"Plugins:    no approved list a",
		"Boot unit:  can't ask systemd:",
	)
	for _, l := range strings.Split(out, "\n") {
		if len([]rune(l)) > 30 {
			t.Errorf("line not cut to 30: %q", l)
		}
	}
}

// A tree this login can't read says why: the jar line says so rather than
// that there's no paper.jar, and the trees say to log in again.
func TestStatusTreeDenied(t *testing.T) {
	defer testid.Line(t, "df74y")
	if os.Geteuid() == 0 {
		t.Skip("root reads the tree anyway")
	}
	g := newRig(t)
	g.p.Cfg.RunGroup = "minecraftd"
	g.p.Group = func(group, what string) error {
		return errors.New("this login isn't in group " + group + " yet, so it can't read " + what + "; log in again")
	}
	if out := g.text(t, 0); strings.Contains(out, "log in again") {
		t.Errorf("group line with nothing refused:\n%s", out)
	}
	if err := os.Chmod(g.dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(g.dir, 0o700) })
	out := g.text(t, 0)
	wantLines(t, out, "            this login isn't in group minecraftd yet, so it can't read the server tree; log in again")
	for _, w := range []string{"Version:    can't read paper.jar: lstat " + g.dir + "/paper.jar: permission denied", "Whitelist:  can't read it:", "Log:        can't read it:"} {
		if !strings.Contains(out, w) {
			t.Errorf("lacks %q:\n%s", w, out)
		}
	}
	g.p.Group = nil
	if out := g.text(t, 0); strings.Contains(out, "log in again") {
		t.Errorf("group line with no Group:\n%s", out)
	}
}

func TestTailFile(t *testing.T) {
	defer testid.Line(t, "df5yb")
	file := filepath.Join(t.TempDir(), "latest.log")
	big := strings.Repeat("x", 100<<10) + "\nnext to last\nlast\r\n\n"
	write(t, file, big)
	got, err := tailFile(file, 3)
	if err != nil || len(got) != 2 || got[0] != "next to last" || got[1] != "last" {
		t.Errorf("tail = %q, %v; want the 2 short lines", got, err)
	}
	write(t, file, "")
	if got, err := tailFile(file, 3); err != nil || len(got) != 0 {
		t.Errorf("empty log: %q, %v", got, err)
	}
}

// The server owns its tree, so a pipe or a device in place of a file it reads
// mustn't hang status or feed it without end.
func TestStatusHostileFiles(t *testing.T) {
	defer testid.Line(t, "df5yh")
	g := newRig(t)
	for _, f := range []string{"ops.json", "logs/latest.log", "plugins/floodgate.jar"} {
		path := filepath.Join(g.dir, f)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"whitelist.json", "plugins/Geyser-Spigot.jar"} {
		path := filepath.Join(g.dir, f)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/zero", path); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan string)
	go func() { done <- g.text(t, 0) }()
	select {
	case out := <-done:
		wantLines(t, out, "Plugins:    3 jars, NOT all approved, so start refuses; changed: plugins/Geyser-Spigot.jar, plugins/floodgate.jar")
		for _, w := range []string{"Whitelist:  can't read it:", "Ops:        can't read them:", "Log:        can't read it:"} {
			if !strings.Contains(out, w) {
				t.Errorf("lacks %q:\n%s", w, out)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("status hung on a pipe or a device")
	}
}
