// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

const doneLine = "[12:00:09 INFO]: Done (8.1s)! For help, type \"help\"\n"

// nativeRig is the Docker rig's tree with a Native on it. The unit's state
// follows the states list, and launch is what Paper does once systemd-run runs.
type nativeRig struct {
	*rig
	n      *Native
	launch func()
}

func newNativeRig(t *testing.T) *nativeRig {
	t.Helper()
	g := &nativeRig{rig: newRig(t)}
	g.n = &Native{Kit: g.d.Kit}
	g.r.Reply = func(c host.Cmd) ([]byte, error) {
		switch {
		case c.Args[0] == "systemd-run" && g.launch != nil:
			g.launch()
		case c.Args[0] == "systemctl" && c.Args[1] == "show":
			c.Args = []string{"docker", "ps", "state"}
		}
		return g.reply(c)
	}
	if err := os.Mkdir(filepath.Join(g.dir, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	return g
}

// writeLog puts a new latest.log in place, as Paper does when it starts.
func (g *nativeRig) writeLog(t *testing.T, text string) {
	t.Helper()
	path := filepath.Join(g.dir, "logs", "latest.log")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeStart(t *testing.T) {
	defer testid.Line(t, "df5y1")
	g := newNativeRig(t)
	g.n.Cfg.ExtraArgs = []string{"--world-dir=$HOME"}
	g.n.Cfg.Nice = 5
	g.states = []string{"inactive", "active"}
	g.writeLog(t, "an old run\n"+doneLine)
	g.launch = func() { g.writeLog(t, doneLine) }
	if err := g.n.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { return host.Quote([]string{s}) }
	paper, geyser := g.dir+"/paper-26.3-244.jar", g.dir+"/plugins/Geyser-Spigot.jar"
	cfg := g.n.Cfg
	run := []string{"systemd-run", "--unit=tt-minecraftd-server.service", "--description=tt-minecraftd Paper server", "--collect",
		"--uid=minecraftd", "--gid=minecraftd", "--working-directory=" + g.dir, "--nice=5",
		"-p", "Type=forking", "-p", "UMask=0007",
		"-p", "RuntimeDirectory=tt-minecraftd", "-p", "RuntimeDirectoryMode=0750",
		"-p", "ProtectSystem=strict", "-p", "ProtectHome=yes", "-p", "PrivateTmp=yes",
		"-p", "PrivateDevices=yes", "-p", "NoNewPrivileges=yes", "-p", "CapabilityBoundingSet=",
		"-p", "MemoryMax=6442450944", "-p", "MemorySwapMax=0", "-p", "TasksMax=2048",
		"-p", "OOMScoreAdjust=1000", "-p", "TimeoutStopSec=30",
		"-p", "ReadWritePaths=" + g.dir, "-p", "ReadOnlyPaths=" + paper, "-p", "ReadOnlyPaths=" + geyser,
		"--", "tmux", "-S", "/run/tt-minecraftd/tmux.sock", "-f", "/dev/null", "new-session", "-d", "-s", "tt-minecraftd", "--",
		"java", "-Xms1G", "-Xmx4G"}
	run = append(run, cfg.JvmArgs...)
	run = append(run, "-Dlog4j2.formatMsgNoLookups=true", "-jar", "paper.jar", "--nogui", "--world-dir=$$HOME")
	want := []string{
		"sudo -v",
		"sudo chown -R -- minecraftd:minecraftd " + q(g.dir),
		"sudo chmod -R ug+rwX,o-rwx -- " + q(g.dir),
		"sudo chown root:root -- " + q(paper) + " " + q(geyser),
		"sudo chmod 0644 -- " + q(paper) + " " + q(geyser),
		"sudo " + host.Quote(run),
	}
	if got := g.changes(); !reflect.DeepEqual(got, want) {
		t.Errorf("ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(g.out.String(), "The server is up.") || g.slept != 0 {
		t.Errorf("output %q, slept %s", g.out.String(), g.slept)
	}
}

// The last run's latest.log, Done line and all, isn't taken for this run's.
// A server that dies shows this run's log tail, or says there's none.
func TestNativeStartLog(t *testing.T) {
	defer testid.Line(t, "df5y2")
	g := newNativeRig(t)
	g.states = []string{"inactive", "active"}
	g.writeLog(t, doneLine)
	err := g.n.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "didn't finish loading") || !strings.Contains(err.Error(), "logs/latest.log") || g.slept != 5*time.Second {
		t.Errorf("stale log: err %v, slept %s", err, g.slept)
	}

	g = newNativeRig(t)
	g.states = []string{"inactive", "inactive"}
	g.writeLog(t, doneLine)
	err = g.n.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "before it wrote a log") {
		t.Errorf("no log: err %v", err)
	}

	g = newNativeRig(t)
	g.states = []string{"inactive", "inactive"}
	g.launch = func() { g.writeLog(t, strings.Repeat("filler\n", 30)+"java.lang.OutOfMemoryError\n") }
	err = g.n.Start(context.Background(), StartMode{})
	if err == nil || !strings.Contains(err.Error(), "exited while loading") || !strings.Contains(g.out.String(), "OutOfMemoryError") ||
		strings.Count(g.out.String(), "filler") != 19 {
		t.Errorf("died: err %v, output %q", err, g.out.String())
	}
}

// Another Paper server, a live unit, or a path the unit can't take stop start
// before it changes anything. A failed unit is cleared.
func TestNativeStartRefuses(t *testing.T) {
	defer testid.Line(t, "df5y3")
	g := newNativeRig(t)
	g.ps = "safeserver 4242 /usr/bin/java -jar /srv/old/paper.jar nogui\n"
	if err := g.n.Start(context.Background(), StartMode{}); err == nil || len(g.r.Ran) != 1 {
		t.Errorf("paper running: err %v, ran %q", err, g.r.Lines())
	}

	g = newNativeRig(t)
	g.states = []string{"activating"}
	if err := g.n.Start(context.Background(), StartMode{}); err == nil || !strings.Contains(err.Error(), "already activating") || len(g.changes()) != 1 {
		t.Errorf("unit up: err %v, ran %q", err, g.changes())
	}

	g = newNativeRig(t)
	spaced := filepath.Join(filepath.Dir(g.dir), "world two")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	g.n.Cfg.OwnerTrees = append(g.n.Cfg.OwnerTrees, spaced)
	if err := g.n.Start(context.Background(), StartMode{}); err == nil || !strings.Contains(err.Error(), "can't hold a space") || len(g.changes()) != 1 {
		t.Errorf("spaced tree: err %v, ran %q", err, g.changes())
	}

	g = newNativeRig(t)
	g.n.DryRun = true
	g.states = []string{"failed"}
	if err := g.n.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	if got := g.changes(); len(got) != 7 || got[1] != "sudo systemctl reset-failed tt-minecraftd-server.service" {
		t.Errorf("failed unit: ran %q", got)
	}
}

// --debug attaches to tmux as the service account. A detach leaves the trees
// with the server, and an exit hands them back. --foreground waits for the
// unit to end, then hands back. Dry run waits for nothing.
func TestNativeStartModes(t *testing.T) {
	defer testid.Line(t, "df5y4")
	g := newNativeRig(t)
	g.states = []string{"inactive", "active"}
	if err := g.n.Start(context.Background(), StartMode{Debug: true}); err != nil {
		t.Fatal(err)
	}
	got := g.changes()
	last := g.r.Ran[len(g.r.Ran)-2]
	if len(got) != 7 || got[6] != "sudo -u minecraftd -- tmux -S /run/tt-minecraftd/tmux.sock attach-session -t tt-minecraftd" || !last.Tty ||
		!strings.Contains(g.out.String(), "Detached") {
		t.Errorf("debug, detached: ran %q, output %q", got, g.out.String())
	}

	g = newNativeRig(t)
	g.states = []string{"inactive", "inactive"}
	if err := g.n.Start(context.Background(), StartMode{Debug: true}); err != nil {
		t.Fatal(err)
	}
	if got := g.changes(); len(got) != 8 || !strings.Contains(got[7], "chown -R -- alice:minecraftd") {
		t.Errorf("debug, exited: ran %q", got)
	}

	g = newNativeRig(t)
	g.n.Root = true
	g.states = []string{"inactive", "active", "active", "inactive"}
	g.launch = func() { g.writeLog(t, doneLine) }
	if err := g.n.Start(context.Background(), StartMode{Foreground: true}); err != nil {
		t.Fatal(err)
	}
	got = g.changes()
	if strings.Contains(strings.Join(g.r.Lines(), "\n"), "sudo -v") || !strings.Contains(got[len(got)-1], "chown -R -- alice:minecraftd") || g.slept != 2*foregroundPoll {
		t.Errorf("foreground: ran %q, slept %s", g.r.Lines(), g.slept)
	}

	g = newNativeRig(t)
	g.n.DryRun = true
	if err := g.n.Start(context.Background(), StartMode{}); err != nil {
		t.Fatal(err)
	}
	if last := g.r.Lines()[len(g.r.Ran)-1]; !strings.HasPrefix(last, "sudo systemd-run ") || g.slept != 0 {
		t.Errorf("dry run ended with %q, slept %s", last, g.slept)
	}
}

// stop sends stop to the console and hands back. A server that hangs gets
// systemctl stop, which sends SIGTERM, then SIGKILL after TimeoutStopSec.
func TestNativeStop(t *testing.T) {
	defer testid.Line(t, "df5y5")
	g := newNativeRig(t)
	g.states = []string{"active", "active", "inactive"}
	if err := g.n.Stop(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	wantSent := []string{"say The server stops in 3 seconds.", "say The server stops in 2 seconds.", "say The server stops in 1 second.", "stop"}
	if !reflect.DeepEqual(g.console.sent, wantSent) {
		t.Errorf("sent %q", g.console.sent)
	}
	want := []string{"sudo -v", "sudo chown -R -- alice:minecraftd " + host.Quote([]string{g.dir})}
	if got := g.changes(); !reflect.DeepEqual(got, want) || g.slept != 4*time.Second {
		t.Errorf("ran %q, slept %s", got, g.slept)
	}

	g = newNativeRig(t)
	g.states = []string{"active"}
	if err := g.n.Stop(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := g.changes(); len(got) != 3 || got[1] != "sudo systemctl stop tt-minecraftd-server.service" {
		t.Errorf("stuck: ran %q", got)
	}

	g = newNativeRig(t)
	g.states = []string{"inactive"}
	if err := g.n.Stop(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	if len(g.console.sent) != 0 || len(g.changes()) != 2 {
		t.Errorf("not running: sent %q, ran %q", g.console.sent, g.changes())
	}
}
