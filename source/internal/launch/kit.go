// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/firewall"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/tree"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/zfs"
)

// Kit is what both modes share: the host, the console, and the config.
type Kit struct {
	Run     host.Runner
	Console Console
	Cfg     *config.Config
	// PluginsFile is the approved jar list, plugins.shcl.
	PluginsFile string
	Out         io.Writer
	Root        bool
	DryRun      bool
	Sleep       func(time.Duration)
	// Now names the snapshots. Nil is time.Now.
	Now func() time.Time
	// Boot is set when the boot unit is enabled for this config, so start and
	// stop go through systemctl. Inside the unit it's never set.
	Boot   bool
	sudoed bool
}

// Running says whether the server runs in the configured mode.
func Running(ctx context.Context, r host.Runner, c *config.Config) (bool, error) {
	if c.RunMode == "native" {
		return (&Native{Kit: Kit{Run: r, Cfg: c}}).running(ctx)
	}
	_, state, err := FindContainer(ctx, r, c.RunMode)
	return state == "running", err
}

// engine is the part of a mode that the shared start and stop steps ask about.
type engine interface {
	running(ctx context.Context) (bool, error)
	// log is the server's output so far, for its Done line.
	log(ctx context.Context) ([]byte, error)
	// tail is the end of log, to show why the server died.
	tail(ctx context.Context) []byte
	// logHint says where to look when loading takes too long.
	logHint() string
	// kill stops a server that didn't stop by itself: SIGTERM, then SIGKILL
	// after killGrace.
	kill(ctx context.Context) error
}

func (k *Kit) say(format string, a ...any) {
	fmt.Fprintf(k.Out, format+"\n", a...)
}

func (k *Kit) warn(warns []string) {
	for _, w := range warns {
		k.say("warning: %s", w)
	}
}

// sudoUp asks for sudo once, before the first command that needs it.
func (k *Kit) sudoUp(ctx context.Context) error {
	if k.Root || k.sudoed {
		return nil
	}
	_, err := k.Run.Run(ctx, host.Cmd{Args: []string{"sudo", "-v"}, Tty: true})
	k.sudoed = err == nil
	return err
}

// refuseOthers stops a start when any Paper server runs, under any account,
// the old scripts' included.
func (k *Kit) refuseOthers(ctx context.Context) error {
	ps, err := k.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"ps", "-eo", "user=,pid=,args="}})
	if err != nil {
		return err
	}
	if found := server.RunningPaper(ps); len(found) > 0 {
		return fmt.Errorf("a Paper server is already running (%s); stop it first", strings.Join(found, "; "))
	}
	return nil
}

// checkSettings refuses a start when a safe setting is off, and warns about
// what's allowed but likely a mistake.
func (k *Kit) checkSettings() error {
	problems, err := settings.Check(k.Cfg)
	if err != nil {
		return k.denied(fmt.Errorf("can't check the safe settings: %w", err))
	}
	k.warn(settings.Warnings(k.Cfg.ServerDir))
	if len(problems) == 0 {
		return nil
	}
	for _, p := range problems {
		k.say("unsafe: %s", p)
	}
	return errors.New("the safe settings aren't in place; sudo tt-minecraftd repair sets them")
}

// denied gives the reason for a read of the tree that was refused, when it's
// a group this login doesn't have yet. Container start reads the tree before
// it changes hands, which after a crash is still the server's.
func (k *Kit) denied(err error) error {
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	if why := k.groupProblem("the server tree"); why != nil {
		return why
	}
	return err
}

// checkPlugins refuses a start when a jar is new or changed since it was
// approved. See design.md, Plugin pinning.
func (k *Kit) checkPlugins() error {
	approved, warns, err := plugins.ReadApproved(k.PluginsFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Nothing approved yet, so every jar is new.
	case err != nil:
		return fmt.Errorf("can't read the approved plugin list: %w", err)
	}
	for _, w := range warns {
		k.say("warning: %s", w)
	}
	c, err := plugins.CheckJars(k.Cfg.ServerDir, approved, &plugins.Sums{})
	if err != nil {
		return k.denied(fmt.Errorf("can't check the plugin jars: %w", err))
	}
	if c.OK() {
		return nil
	}
	for _, name := range c.New {
		k.say("not approved: %s", name)
	}
	for _, name := range c.Changed {
		k.say("changed since it was approved: %s", name)
	}
	return errors.New("a jar is new or changed; sudo tt-minecraftd repair --approve-plugins shows each one and asks")
}

// checkFirewall refuses a start without the tool's nftables table, so the
// server never runs unfenced. See design.md, Firewall.
func (k *Kit) checkFirewall(ctx context.Context) error {
	if !k.Cfg.Firewall {
		return nil
	}
	up, err := firewall.Loaded(ctx, k.Run)
	if err != nil {
		return err
	}
	if !up {
		return fmt.Errorf("the firewall isn't loaded (nftables table inet %s); sudo tt-minecraftd repair loads it", firewall.Table)
	}
	return nil
}

// snapshot takes a snapshot of zfsDataset, when it's set, and prunes. See
// design.md, ZFS snapshots.
func (k *Kit) snapshot(ctx context.Context, kind string) error {
	if k.Cfg.ZfsDataset == "" {
		return nil
	}
	now := time.Now
	if k.Now != nil {
		now = k.Now
	}
	s := zfs.Snapper{Run: k.Run, Dataset: k.Cfg.ZfsDataset, Root: k.Root}
	warns, err := s.Take(ctx, kind, now())
	k.warn(warns)
	return err
}

func (k *Kit) waitLoaded(ctx context.Context, e engine) error {
	k.say("Waiting for the server to load, up to %s.", k.Cfg.StartTimeout)
	for range int(k.Cfg.StartTimeout / time.Second) {
		log, err := e.log(ctx)
		if err != nil {
			return err
		}
		if server.Loaded(log) {
			k.say("The server is up.")
			return nil
		}
		up, err := e.running(ctx)
		if err != nil {
			return err
		}
		if !up {
			tail := strings.TrimRight(string(e.tail(ctx)), "\n")
			if tail == "" {
				return errors.New("the server exited while loading, before it wrote a log; check that Java runs and paper.jar can be read")
			}
			k.say("%s", tail)
			return errors.New("the server exited while loading")
		}
		k.Sleep(time.Second)
	}
	return fmt.Errorf("the server didn't finish loading in %s; it's still running, so see status or %s", k.Cfg.StartTimeout, e.logHint())
}

// stop runs design.md's stop steps that are built so far. warn is the
// countdown for the players, in seconds.
func (k *Kit) stop(ctx context.Context, e engine, warn int) error {
	// Container mode asks the runtime through sudo.
	if err := k.sudoUp(ctx); err != nil {
		return err
	}
	up, err := e.running(ctx)
	if err != nil {
		return err
	}
	if !up {
		k.say("The server isn't running.")
		return k.handBack(ctx)
	}
	if err := k.countDown(ctx, warn); err != nil {
		return err
	}
	if k.Boot {
		// The unit's ExecStop is stop, which saves and snapshots as root.
		if _, err := k.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{"systemctl", "stop", boot.UnitName}}); err != nil {
			return err
		}
		if k.DryRun {
			return k.handBack(ctx)
		}
		if up, err = e.running(ctx); err != nil {
			return err
		}
		if !up {
			return k.handBack(ctx)
		}
		// Not the unit's server, such as one started before it was enabled.
	}
	if err := k.Console.Send(ctx, "stop"); err != nil {
		return err
	}
	if !k.DryRun {
		if err := k.waitStopped(ctx, e); err != nil {
			return err
		}
	}
	// The trees go back even without the snapshot.
	return errors.Join(k.snapshot(ctx, zfs.PostStop), k.handBack(ctx))
}

// startUnit is start when the boot unit is enabled. The checks that can
// refuse a start run here too, so a refusal shows here and not only in the
// journal.
func (k *Kit) startUnit(ctx context.Context) error {
	if err := k.sudoUp(ctx); err != nil {
		return err
	}
	if err := k.checkSettings(); err != nil {
		return err
	}
	if err := k.checkPlugins(); err != nil {
		return err
	}
	if err := k.checkFirewall(ctx); err != nil {
		return err
	}
	if _, err := k.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{"systemctl", "start", boot.UnitName}}); err != nil {
		return err
	}
	if !k.DryRun {
		k.say("The boot unit is enabled, so %s starts the server. status shows when it's up, and journalctl -u %s how it went.", boot.UnitName, boot.UnitName)
	}
	return nil
}

// countDown tells the players at the start, then at each mark below it.
func (k *Kit) countDown(ctx context.Context, warn int) error {
	if warn <= 0 {
		return nil
	}
	marks := []int{warn}
	for _, m := range []int{60, 30, 10, 5, 4, 3, 2, 1} {
		if m < warn {
			marks = append(marks, m)
		}
	}
	marks = append(marks, 0)
	for i, m := range marks[:len(marks)-1] {
		unit := "seconds"
		if m == 1 {
			unit = "second"
		}
		if err := k.Console.Send(ctx, fmt.Sprintf("say The server stops in %d %s.", m, unit)); err != nil {
			return err
		}
		if !k.DryRun {
			k.Sleep(time.Duration(m-marks[i+1]) * time.Second)
		}
	}
	return nil
}

func (k *Kit) waitStopped(ctx context.Context, e engine) error {
	k.say("Waiting for the server to save and stop, up to %s.", k.Cfg.StopTimeout)
	for range int(k.Cfg.StopTimeout / time.Second) {
		up, err := e.running(ctx)
		if err != nil {
			return err
		}
		if !up {
			k.say("The server stopped.")
			return nil
		}
		k.Sleep(time.Second)
	}
	k.say("The server is still up after %s; sending SIGTERM.", k.Cfg.StopTimeout)
	return e.kill(ctx)
}

func (k *Kit) handBack(ctx context.Context) error {
	warns, err := tree.ToOperator(ctx, k.Run, k.Cfg)
	k.warn(warns)
	return err
}
