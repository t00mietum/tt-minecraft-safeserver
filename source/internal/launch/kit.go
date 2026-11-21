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
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/tree"
)

// Kit is what both modes share: the host, the console, and the config.
type Kit struct {
	Run     host.Runner
	Console Console
	Cfg     *config.Config
	// FirewallFiles are the firewall's files, which tests point elsewhere.
	FirewallFiles firewall.Files
	Out           io.Writer
	Root          bool
	DryRun        bool
	// Sleep stands in for the clock in tests. Nil is the real one, which
	// Ctrl-C cuts short.
	Sleep func(time.Duration)
	// slept is the clock under Sleep: what it has been asked to wait so far.
	slept time.Duration
	// Now named the start and stop snapshots, which are gone.
	// Now func() time.Time
	// Boot is set when the boot unit is enabled for this config, so start and
	// stop go through systemctl. Inside the unit it's never set.
	Boot bool
	// InUnit is set inside the boot unit, as start --foreground and its
	// ExecStop run. There start leaves the hand-back to ExecStop's stop.
	InUnit bool
	sudoed bool
}

// ErrInterrupted is a wait cut short by Ctrl-C or SIGTERM.
var ErrInterrupted = errors.New("interrupted")

// errSlowLoad is a server that's still up but hasn't finished loading.
var errSlowLoad = errors.New("the server didn't finish loading")

// stopOff goes to the players when Ctrl-C ends the countdown.
const stopOff = "say The stop is off. The server stays up."

// pause waits d, or until ctx ends.
func (k *Kit) pause(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return ErrInterrupted
	}
	if k.Sleep != nil {
		k.Sleep(d)
		k.slept += d
	} else {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
	if ctx.Err() != nil {
		return ErrInterrupted
	}
	return nil
}

func (k *Kit) now() time.Time {
	if k.Sleep != nil {
		return time.Time{}.Add(k.slept)
	}
	return time.Now()
}

// tick is the next poll's pause: a second, or what's left before the deadline.
func (k *Kit) tick(deadline time.Time) time.Duration {
	return max(0, min(time.Second, deadline.Sub(k.now())))
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

// say cleans what it's given, since jar names, settings values, and the log
// tail come from the server account.
func (k *Kit) say(format string, a ...any) {
	clean := make([]any, len(a))
	for i, v := range a {
		switch v.(type) {
		case string, error, fmt.Stringer:
			clean[i] = server.CleanLines(fmt.Sprint(v))
		default:
			clean[i] = v
		}
	}
	fmt.Fprintf(k.Out, format+"\n", clean...)
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
	// sudo -v wants every matching rule to be NOPASSWD, so it can ask even
	// when sudo -n works, and fails with no terminal.
	if _, err := k.Run.Run(ctx, host.Cmd{Args: []string{"sudo", "-n", "true"}, Look: true, Quiet: true}); err == nil {
		k.sudoed = true
		return nil
	}
	// Not a look: dry run prints it and never asks (2026111715275268).
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

// checkFirewall refuses a start without the tool's nftables table, or with
// rules written for another config or another network, so the server never
// runs unfenced. The file stands in for the table, since the unit loads it
// and repair reloads it whenever it writes it. See design.md, Firewall.
func (k *Kit) checkFirewall(ctx context.Context) error {
	if !k.Cfg.Firewall {
		return nil
	}
	same, err := firewall.Matches(k.Cfg, k.FirewallFiles)
	if err != nil {
		return fmt.Errorf("can't check the firewall rules: %w", err)
	}
	if !same {
		return errors.New("the firewall rules don't match the config or the host's network; sudo tt-minecraftd repair writes and loads them")
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

// // snapshot takes a snapshot of zfsDataset, when it's set, and prunes. See
// // design.md, ZFS snapshots.
// func (k *Kit) snapshot(ctx context.Context, kind string) error {
// 	if k.Cfg.ZfsDataset == "" {
// 		return nil
// 	}
// 	now := time.Now
// 	if k.Now != nil {
// 		now = k.Now
// 	}
// 	s := zfs.Snapper{Run: k.Run, Dataset: k.Cfg.ZfsDataset, Root: k.Root}
// 	warns, err := s.Take(ctx, kind, now())
// 	k.warn(warns)
// 	return err
// }

func (k *Kit) waitLoaded(ctx context.Context, e engine) error {
	k.say("Waiting for the server to load, up to %s.", k.Cfg.StartTimeout)
	deadline := k.now().Add(k.Cfg.StartTimeout)
	for k.now().Before(deadline) {
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
		if err := k.pause(ctx, k.tick(deadline)); err != nil {
			return fmt.Errorf("%w while the server loads; it's still running, so see status or %s", err, e.logHint())
		}
	}
	return fmt.Errorf("%w in %s; it's still running, so see status or %s", errSlowLoad, k.Cfg.StartTimeout, e.logHint())
}

// slowAtBoot says whether a failed wait for the load is only a warning, and
// gives it. In --foreground the server outlives a slow load: an exit there
// ends the boot unit, and systemd then runs its ExecStop, so a big world or
// a slow host would never come up at boot.
func (k *Kit) slowAtBoot(mode StartMode, err error) bool {
	if !mode.Foreground || !errors.Is(err, errSlowLoad) {
		return false
	}
	k.say("warning: %s", err)
	return true
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
		// The unit's ExecStop is stop, which saves as root.
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
	// No stop snapshot any more (backlog 2026111318002875).
	// return errors.Join(k.snapshot(ctx, zfs.PostStop), k.handBack(ctx))
	return k.handBack(ctx)
}

// preflight is the checks that can refuse a start, in every mode.
func (k *Kit) preflight(ctx context.Context) error {
	if err := k.checkSettings(); err != nil {
		return err
	}
	return k.checkFirewall(ctx)
}

// startUnit is start when the boot unit is enabled. The checks that can
// refuse a start run here too, so a refusal shows here and not only in the
// journal.
func (k *Kit) startUnit(ctx context.Context) error {
	if err := k.sudoUp(ctx); err != nil {
		return err
	}
	if err := k.preflight(ctx); err != nil {
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
			if ctx.Err() != nil {
				return k.callOff(ctx)
			}
			return err
		}
		if !k.DryRun {
			if err := k.pause(ctx, time.Duration(m-marks[i+1])*time.Second); err != nil {
				return k.callOff(ctx)
			}
		}
	}
	return nil
}

// callOff tells the players the stop is off, after Ctrl-C ended the
// countdown. ctx is done by then, so the line goes out on one of its own.
func (k *Kit) callOff(ctx context.Context) error {
	tell, cancel := context.WithTimeout(context.WithoutCancel(ctx), attachTimeout)
	defer cancel()
	err := fmt.Errorf("%w; the server wasn't stopped", ErrInterrupted)
	if sendErr := k.Console.Send(tell, stopOff); sendErr != nil {
		return errors.Join(err, fmt.Errorf("the players weren't told the stop is off: %w", sendErr))
	}
	return err
}

func (k *Kit) waitStopped(ctx context.Context, e engine) error {
	k.say("Waiting for the server to save and stop, up to %s.", k.Cfg.StopTimeout)
	deadline := k.now().Add(k.Cfg.StopTimeout)
	for k.now().Before(deadline) {
		up, err := e.running(ctx)
		if err != nil {
			return err
		}
		if !up {
			k.say("The server stopped.")
			return nil
		}
		if err := k.pause(ctx, k.tick(deadline)); err != nil {
			return fmt.Errorf("%w while the server saves; it's still stopping, and stop again hands the trees back once it's down", err)
		}
	}
	k.say("The server is still up after %s; sending SIGTERM.", k.Cfg.StopTimeout)
	return e.kill(ctx)
}

// startHandBack is the hand-back at the end of a start. Inside the boot unit
// it's left to ExecStop's stop. systemd runs that after every start, even
// when the server ended by itself, and sends SIGTERM to the unit once stop
// is done, which would cut a second chown short and fail the unit.
func (k *Kit) startHandBack(ctx context.Context) error {
	if k.InUnit {
		return nil
	}
	return k.handBack(ctx)
}

// failedStart is a start that failed after the hand-off. With the server
// down, the trees come back, so they don't stay with the service account. A
// slow or cut-short load leaves the server up, and its error says so.
func (k *Kit) failedStart(ctx context.Context, e engine, err error) error {
	if k.InUnit || errors.Is(err, errSlowLoad) || errors.Is(err, ErrInterrupted) {
		return err
	}
	// After Ctrl-C a chown would get the same SIGINT.
	if ctx.Err() == nil {
		if up, upErr := e.running(ctx); upErr == nil && !up {
			return errors.Join(err, k.handBack(ctx))
		}
	}
	return fmt.Errorf("%w; the trees are still %s's, and stop hands them back", err, k.Cfg.RunUser)
}

// stoppedByUnit says whether start --foreground's wait for the server's end
// was cut short only by systemd's SIGTERM, after ExecStop's stop brought the
// server down. That's a clean stop, not a failure of the unit.
func (k *Kit) stoppedByUnit(ctx context.Context, e engine) bool {
	if !k.InUnit || ctx.Err() == nil {
		return false
	}
	look, cancel := context.WithTimeout(context.WithoutCancel(ctx), attachTimeout)
	defer cancel()
	up, err := e.running(look)
	return err == nil && !up
}

func (k *Kit) handBack(ctx context.Context) error {
	warns, err := tree.ToOperator(ctx, k.Run, k.Cfg)
	k.warn(warns)
	return err
}
