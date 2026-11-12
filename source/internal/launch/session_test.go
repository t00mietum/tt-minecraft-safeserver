// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// console checks the server is up, says how to leave, and hands the terminal
// over with Ctrl-C as the way out. An exit hands the trees back; a detach
// doesn't.
func TestOpenConsole(t *testing.T) {
	defer testid.Line(t, "df5z6")
	g := newRig(t)
	g.states = []string{"running"}
	if err := g.d.OpenConsole(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"sudo -v", "sudo docker attach --sig-proxy=false --detach-keys=ctrl-c tt-minecraftd"}
	out := g.out.String()
	if got := g.changes(); !reflect.DeepEqual(got, want) || !g.r.Ran[len(g.r.Ran)-2].Tty ||
		!strings.HasPrefix(out, consoleHello) || !strings.Contains(out, "Detached") {
		t.Errorf("docker, detached: ran %q, output %q", got, out)
	}

	// docker attach exits 1 after the detach keys. Any other failure is an error.
	for code, wantErr := range map[int]bool{1: false, 2: true} {
		g = newRig(t)
		g.states = []string{"running"}
		exit := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
		g.r.Reply = func(c host.Cmd) ([]byte, error) {
			if c.Args[1] == "attach" {
				return nil, fmt.Errorf("docker attach: %w", exit)
			}
			return g.reply(c)
		}
		err := g.d.OpenConsole(context.Background())
		if (err != nil) != wantErr || strings.Contains(g.out.String(), "Detached") == wantErr {
			t.Errorf("docker attach exit %d: err %v, output %q", code, err, g.out.String())
		}
	}

	g = newRig(t)
	g.states = []string{"running", "running", "exited"}
	if err := g.d.OpenConsole(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := g.changes(); len(got) != 3 || !strings.Contains(got[2], "chown -R -- alice:minecraftd") {
		t.Errorf("docker, stopped from the console: ran %q", got)
	}

	g = newRig(t)
	g.states = []string{"exited"}
	if err := g.d.OpenConsole(context.Background()); err == nil || !strings.Contains(err.Error(), "isn't running") || len(g.changes()) != 1 {
		t.Errorf("docker, not running: err %v, ran %q", err, g.changes())
	}

	n := newNativeRig(t)
	n.states = []string{"active"}
	if err := n.n.OpenConsole(context.Background()); err != nil {
		t.Fatal(err)
	}
	want = []string{"sudo -u minecraftd -- tmux -S /run/tt-minecraftd/tmux.sock bind-key -n C-c detach-client ';' attach-session -t tt-minecraftd"}
	if got := n.changes(); !reflect.DeepEqual(got, want) || !n.r.Ran[1].Tty || !strings.Contains(n.out.String(), "Detached") {
		t.Errorf("native: ran %q, output %q", got, n.out.String())
	}

	n = newNativeRig(t)
	n.states = []string{"inactive"}
	if err := n.n.OpenConsole(context.Background()); err == nil || len(n.changes()) != 0 {
		t.Errorf("native, not running: err %v, ran %q", err, n.changes())
	}
}

// cmdRig is a running server whose log grows by the lines in feed, one entry
// per poll, after the command is sent.
type cmdRig struct {
	*nativeRig
	log   string
	feed  []string
	polls int
}

func newCmdRig(t *testing.T, old string) *cmdRig {
	t.Helper()
	g := &cmdRig{nativeRig: newNativeRig(t)}
	g.log = filepath.Join(g.dir, "logs", "latest.log")
	if old != "" {
		g.writeLog(t, old)
	}
	g.states = []string{"active"}
	g.n.Sleep = func(d time.Duration) {
		if d != answerPoll {
			t.Errorf("slept %s", d)
		}
		if g.polls < len(g.feed) {
			g.add(t, g.feed[g.polls])
		}
		g.polls++
	}
	return g
}

func (g *cmdRig) add(t *testing.T, text string) {
	t.Helper()
	f, err := os.OpenFile(g.log, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// cmd prints only the lines after the command, whole, joins a line written
// in two parts, drops escape codes, and stops once the answer goes quiet.
func TestSendCmd(t *testing.T) {
	defer testid.Line(t, "df5z7")
	g := newCmdRig(t, "[12:00:01 INFO]: from before\n[12:00:02 INFO]: half wri")
	g.feed = []string{"", "tten\n[12:00:03 INFO]: Added Steve to the whitelist\n[12:00:03 INFO]: \x1b[31mred\r\n[12:00:03 INFO]: par", "t two\n"}
	if err := g.n.SendCmd(context.Background(), "whitelist add Steve"); err != nil {
		t.Fatal(err)
	}
	want := "[12:00:03 INFO]: Added Steve to the whitelist\n[12:00:03 INFO]: [31mred\n[12:00:03 INFO]: part two\n"
	if got := g.out.String(); got != want {
		t.Errorf("output\n%q\nwant\n%q", got, want)
	}
	if !reflect.DeepEqual(g.console.sent, []string{"whitelist add Steve"}) || g.polls != 3+answerQuiet {
		t.Errorf("sent %q, polled %d times", g.console.sent, g.polls)
	}

	// Nothing back: give up after answerFirst polls.
	g = newCmdRig(t, "")
	if err := g.n.SendCmd(context.Background(), "save-on"); err != nil || g.out.Len() != 0 || g.polls != answerFirst {
		t.Errorf("silent: err %v, output %q, polled %d times", err, g.out.String(), g.polls)
	}

	// A new day's log is read from its start.
	g = newCmdRig(t, "[23:59:59 INFO]: yesterday, and long\n")
	g.n.Sleep = func(time.Duration) {
		if g.polls == 1 {
			g.writeLog(t, "[00:00:00 INFO]: new\n")
		}
		g.polls++
	}
	if err := g.n.SendCmd(context.Background(), "list"); err != nil || g.out.String() != "[00:00:00 INFO]: new\n" {
		t.Errorf("new log: err %v, output %q", err, g.out.String())
	}

	// A server that's down, or a command that's stop by another road, sends nothing.
	for _, line := range []string{"stop", " /Stop now", "restart", " "} {
		g = newCmdRig(t, "")
		if err := g.n.SendCmd(context.Background(), line); err == nil || len(g.console.sent) != 0 || len(g.r.Ran) != 0 {
			t.Errorf("%q: err %v, sent %q", line, err, g.console.sent)
		}
	}
	g = newCmdRig(t, "")
	g.states = []string{"inactive"}
	if err := g.n.SendCmd(context.Background(), "list"); err == nil || len(g.console.sent) != 0 {
		t.Errorf("not running: err %v, sent %q", err, g.console.sent)
	}

	// A pipe in place of the log can't hang it.
	g = newCmdRig(t, "")
	if err := syscall.Mkfifo(g.log, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := g.n.SendCmd(context.Background(), "list"); err == nil || len(g.console.sent) != 0 {
		t.Errorf("pipe: err %v, sent %q", err, g.console.sent)
	}

	// The Docker side runs the same steps.
	g = newCmdRig(t, "")
	g.d.Sleep = g.n.Sleep
	g.states = []string{"running"}
	g.feed = []string{"[12:00:03 INFO]: There are 0 of a max of 20 players online\n"}
	if err := g.d.SendCmd(context.Background(), "list"); err != nil || !strings.Contains(g.out.String(), "20 players") {
		t.Errorf("docker: err %v, output %q", err, g.out.String())
	}
}

// Lines from other threads, such as Geyser's just after a start, are shown
// but don't end the wait while the server thread is slow to answer.
func TestSendCmdOtherThreads(t *testing.T) {
	defer testid.Line(t, "df753")
	stun := "[12:00:03] [NetherNet Built-in Signaling-4-1/INFO]: [StunObserver] STUN discovery maps this host\n"
	answer := "[12:00:04] [Server thread/INFO]: There are 0 of a max of 20 players online: \n"
	g := newCmdRig(t, "")
	g.feed = make([]string, 12)
	g.feed[0] = stun
	g.feed[2] = "[12:00:03] [ForkJoinPool.commonPool-worker-2/WARN]: [Geyser-Spigot] slow\n\tat its second line\n"
	g.feed[4] = "\tat a third line, later\n"
	g.feed[10] = answer
	g.feed[11] = "a second line of the answer\n"
	if err := g.n.SendCmd(context.Background(), "list"); err != nil {
		t.Fatal(err)
	}
	out := g.out.String()
	if !strings.Contains(out, "players online") || !strings.HasSuffix(out, "a second line of the answer\n") || g.polls != 12+answerQuiet {
		t.Errorf("slow answer: output %q, polled %d times", out, g.polls)
	}

	// Only other threads: shown, and the wait ends as with nothing back.
	g = newCmdRig(t, "")
	g.feed = []string{stun, "", stun}
	if err := g.n.SendCmd(context.Background(), "list"); err != nil || strings.Count(g.out.String(), "STUN") != 2 || g.polls != answerFirst {
		t.Errorf("other threads only: err %v, output %q, polled %d times", err, g.out.String(), g.polls)
	}
}

// A log removed and made again, as at a rollover, is read from its start,
// even where the new file gets the old one's inode, as on ext4.
func TestLogFollowNewFile(t *testing.T) {
	defer testid.Line(t, "df5zp")
	path := filepath.Join(t.TempDir(), "latest.log")
	if err := os.WriteFile(path, []byte("old line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &logFollow{file: path}
	defer f.close()
	if err := f.skip(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new day\nanswer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := f.next()
	if want := []string{"new day", "answer"}; err != nil || !reflect.DeepEqual(lines, want) {
		t.Errorf("got %q, %v, want %q", lines, err, want)
	}
}
