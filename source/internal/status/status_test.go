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

var now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

const ssOut = `tcp   LISTEN 0 50  *:25565 *:*
tcp   ESTAB  0 0   [::ffff:192.168.1.9]:25565 [::ffff:192.168.1.20]:51000
tcp   ESTAB  0 0   [::ffff:192.168.1.9]:25565 [::ffff:203.0.113.7]:40000
tcp   ESTAB  0 0   192.168.1.9:22 192.168.1.20:50000
tcp   LISTEN 0 50  127.0.0.1:125565 *:*
udp   UNCONN 0 0   0.0.0.0:19132 0.0.0.0:*
`

// rig is a server tree, an approved list, and a fake host.
type rig struct {
	p       *Probe
	r       *host.Fake
	dir     string
	replies map[string]string
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
	write(t, list, "jar: plugins/Geyser-Spigot.jar\n\tsha256: "+sum("geyser")+"\njar: plugins/floodgate.jar\n\tsha256: "+sum("floodgate")+"\n")

	cfg := config.Defaults()
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
	files := map[string]string{
		"/proc/4242/cgroup": "0::/system.slice/docker-abc.scope\n",
		"/sys/fs/cgroup/system.slice/docker-abc.scope/memory.current": "1073741824\n",
	}
	g.p = &Probe{
		Run: g.r, Cfg: &cfg, PluginsFile: list, Now: func() time.Time { return now },
		ReadFile: func(f string) ([]byte, error) {
			if s, ok := files[f]; ok {
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
	g.replies["docker ps"] = "running\n"
	g.replies["docker inspect"] = "2026-11-11T09:47:30.123456789Z 4242 0:0\n"
	out := g.text(t, 0)
	wantLines(t, out,
		"Server:     running in docker mode, as root, for 2h 12m",
		"Version:    Paper 26.3 (paper-26.3-244.jar)",
		"Players:    2 of 20 online: Steve",
		"Memory:     1.0 GiB of 4.0 GiB cap (25%)",
		"Ports:      tcp *:25565 listening, 2 connections",
		"            udp 0.0.0.0:19132 listening",
		"Firewall:   drop counters not set up yet",
		"Whitelist:  3 players",
		"Ops:        Steve, Evil[2J",
		"Plugins:    2 jars, all approved",
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
	g.replies["systemctl show tt-minecraftd-server.service"] = fmt.Sprintf("ActiveState=active\nActiveEnterTimestamp=@%d\nMemoryCurrent=536870912\nUser=minecraftd\n", now.Add(-50*time.Hour).Unix())
	out := g.text(t, 0)
	wantLines(t, out,
		"Server:     running in native mode, as minecraftd, for 2d 2h",
		"Memory:     512 MiB of 4.0 GiB cap (12%)",
	)
	if g.pinged != "192.168.1.9:25565" {
		t.Errorf("pinged %s, want bindIP", g.pinged)
	}
}

// Stopped, with an old script's server still up, a changed jar, and the
// firewall and Bedrock off.
func TestStatusStopped(t *testing.T) {
	defer testid.Line(t, "df5y9")
	g := newRig(t)
	g.replies["docker ps"] = "exited\n"
	g.replies["ps -eo"] = "safeserver 811 /usr/bin/java -Xmx4G -jar paper-26.3-244.jar --nogui\n"
	g.replies["systemctl show tt-minecraftd.service"] = ""
	g.p.Cfg.Firewall, g.p.Cfg.Bedrock = false, false
	write(t, filepath.Join(g.dir, "plugins", "floodgate.jar"), "swapped")
	write(t, filepath.Join(g.dir, "plugins", "new.jar"), "new")
	if err := os.Remove(filepath.Join(g.dir, "ops.json")); err != nil {
		t.Fatal(err)
	}
	out := g.text(t, 0)
	wantLines(t, out,
		"Server:     not running in docker mode; the container is exited",
		"            another Paper server is running: pid 811, as safeserver",
		"Version:    paper-26.3-244.jar",
		"Memory:     none in use; 4.0 GiB cap",
		"Ports:      tcp *:25565 listening, 2 connections",
		"Firewall:   off in the config",
		"Ops:        none",
		"Plugins:    3 jars, NOT all approved, so start refuses; new: plugins/new.jar; changed: plugins/floodgate.jar",
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
		"Server:     can't ask docker: ",
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
		wantLines(t, out, "Plugins:    2 jars, NOT all approved, so start refuses; changed: plugins/Geyser-Spigot.jar, plugins/floodgate.jar")
		for _, w := range []string{"Whitelist:  can't read it:", "Ops:        can't read them:", "Log:        can't read it:"} {
			if !strings.Contains(out, w) {
				t.Errorf("lacks %q:\n%s", w, out)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("status hung on a pipe or a device")
	}
}
