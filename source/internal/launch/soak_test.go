// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/soak"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// start, console, cmd, and stop, over and over against one rig, keep no
// heap or goroutines past the first rounds. TT_MINECRAFTD_SOAK sets the
// rounds, and the test skips without it.
func TestSoak(t *testing.T) {
	defer testid.Line(t, "df78e")
	t.Setenv("PATH", t.TempDir()) // nothing on the host can run
	g := newRig(t)
	logs := filepath.Join(g.dir, "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	latest := filepath.Join(logs, "latest.log")
	answer := ""
	g.d.Sleep = func(d time.Duration) {
		g.slept += d
		if answer == "" {
			return
		}
		f, err := os.OpenFile(latest, os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			_, err = f.WriteString(answer)
			f.Close()
		}
		if err != nil {
			t.Error(err)
		}
		answer = ""
	}
	ctx := context.Background()
	soak.Check(t, func(i int) error {
		g.r.Ran, g.console.sent, g.slept = g.r.Ran[:0], g.console.sent[:0], 0
		g.out.Reset()
		// A new latest.log each start, as Paper does.
		if err := os.Remove(latest); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(latest, []byte("[12:00:01] [Server thread/INFO]: Starting\n"), 0o600); err != nil {
			return err
		}
		g.states = []string{"exited", "running"}
		g.log = "[12:00:09 INFO]: Done (8.1s)! For help, type \"help\"\n"
		if err := g.d.Start(ctx, StartMode{}); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		g.states = []string{"running"}
		if err := g.d.OpenConsole(ctx); err != nil || !strings.Contains(g.out.String(), "Detached") {
			return fmt.Errorf("console: %v, output %q", err, g.out.String())
		}
		g.out.Reset()
		answer = fmt.Sprintf("[12:00:10] [Server thread/INFO]: There are 0 of a max of 20 players online (%d)\n", i)
		if err := g.d.SendCmd(ctx, "list"); err != nil || !strings.Contains(g.out.String(), fmt.Sprintf("online (%d)", i)) {
			return fmt.Errorf("cmd: %v, output %q", err, g.out.String())
		}
		g.states, g.console.sent = []string{"running", "running", "exited"}, g.console.sent[:0]
		if err := g.d.Stop(ctx, 0); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
		if len(g.console.sent) != 1 || g.console.sent[0] != "stop" {
			return fmt.Errorf("sent %q", g.console.sent)
		}
		return nil
	})
}
