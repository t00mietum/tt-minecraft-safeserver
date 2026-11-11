// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/tree"
)

// Unit is the transient unit the server runs in. tt-minecraftd.service is
// the boot unit's name, and that one runs start --foreground.
const Unit = "tt-minecraftd-server.service"

// TmuxSocket is the service account's own tmux server. systemd makes its
// folder, as the unit's RuntimeDirectory.
const TmuxSocket = "/run/tt-minecraftd/tmux.sock"

// foregroundPoll is how often start --foreground looks for the server's end.
const foregroundPoll = 5 * time.Second

// Native runs the server in tmux, under a sandboxed systemd unit. See
// design.md, Run modes.
type Native struct {
	Kit
	// oldLog is latest.log from before the launch, so its Done line isn't
	// taken for this run's.
	oldLog os.FileInfo
}

// Start runs design.md's start steps that are built so far.
func (n *Native) Start(ctx context.Context, mode StartMode) error {
	if err := n.refuseOthers(ctx); err != nil {
		return err
	}
	if !mode.Foreground {
		if err := n.sudoUp(ctx); err != nil {
			return err
		}
	}
	if err := n.checkSettings(); err != nil {
		return err
	}
	// The plugin, firewall, and snapshot steps come with their own items.
	jars, err := lockedJars(n.Cfg.ServerDir)
	if err != nil {
		return err
	}
	args, err := n.runArgs(jars)
	if err != nil {
		return err
	}
	state, err := n.state(ctx)
	if err != nil {
		return err
	}
	switch state {
	case "inactive", "":
	case "failed":
		// --collect drops a failed unit, but one from an older systemd or by hand may linger.
		if _, err := n.Run.Run(ctx, host.Cmd{Args: []string{"systemctl", "reset-failed", Unit}, Sudo: true}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is already %s", Unit, state)
	}
	warns, err := tree.ToServer(ctx, n.Run, n.Cfg)
	n.warn(warns)
	if err != nil {
		return err
	}
	if err := n.lockJars(ctx, jars); err != nil {
		return err
	}
	n.oldLog, _ = os.Stat(n.latestLog()) // none yet is fine
	if _, err := n.Run.Run(ctx, host.Cmd{Args: args, Sudo: true}); err != nil {
		return err
	}

	if mode.Debug {
		return n.attach(ctx)
	}
	if n.DryRun {
		return nil
	}
	if err := n.waitLoaded(ctx, n); err != nil {
		return err
	}
	if !mode.Foreground {
		return nil
	}
	for {
		up, err := n.running(ctx)
		if err != nil {
			return err
		}
		if !up {
			return n.handBack(ctx)
		}
		n.Sleep(foregroundPoll)
	}
}

// attach is --debug: the console in this terminal. A detach leaves the
// server up, and its trees with it.
func (n *Native) attach(ctx context.Context) error {
	n.say("Starting with the console attached. Type stop to stop the server, or press Ctrl-b d to leave it running.")
	_, err := n.Run.Run(ctx, host.Cmd{Args: []string{"tmux", "-S", TmuxSocket, "attach-session", "-t", Container}, As: n.Cfg.RunUser, Tty: true})
	if n.DryRun {
		return err
	}
	up, upErr := n.running(ctx)
	if upErr == nil && up {
		n.say("Detached. The server is still running; stop stops it.")
		return err
	}
	return errors.Join(err, upErr, n.handBack(ctx))
}

// lockJars gives paper.jar and the plugin jars to root while the server
// runs, per design.md's Tree ownership. The unit mounts them read-only too.
func (n *Native) lockJars(ctx context.Context, jars []string) error {
	chown := append([]string{"chown", "root:root", "--"}, jars...)
	if _, err := n.Run.Run(ctx, host.Cmd{Args: chown, Sudo: true}); err != nil {
		return err
	}
	chmod := append([]string{"chmod", "0644", "--"}, jars...)
	_, err := n.Run.Run(ctx, host.Cmd{Args: chmod, Sudo: true})
	return err
}

// runArgs builds the systemd-run command. See design.md, Run modes.
func (n *Native) runArgs(jars []string) ([]string, error) {
	c := n.Cfg
	args := []string{
		"systemd-run", "--unit=" + Unit, "--description=tt-minecraftd Paper server", "--collect",
		"--uid=" + c.RunUser, "--gid=" + c.RunGroup, "--working-directory=" + c.ServerDir, "--nice=" + strconv.Itoa(c.Nice),
		// tmux new-session -d returns once the session is up, and leaves its server behind.
		"-p", "Type=forking",
		"-p", "UMask=0007",
		"-p", "RuntimeDirectory=tt-minecraftd", "-p", "RuntimeDirectoryMode=0750",
		"-p", "ProtectSystem=strict", "-p", "ProtectHome=yes", "-p", "PrivateTmp=yes",
		"-p", "PrivateDevices=yes", "-p", "NoNewPrivileges=yes",
		"-p", "CapabilityBoundingSet=",
		"-p", "MemoryMax=" + strconv.FormatInt(c.MemCap, 10), "-p", "MemorySwapMax=0",
		"-p", "TasksMax=" + strconv.Itoa(pidsLimit),
		"-p", "OOMScoreAdjust=1000",
		"-p", "TimeoutStopSec=" + strconv.Itoa(killGrace),
	}
	for _, t := range c.OwnerTrees {
		real, err := filepath.EvalSymlinks(t)
		if err != nil {
			// tree.ToServer warns about a missing tree.
			continue
		}
		if args, err = appendUnitPath(args, "ReadWritePaths", real); err != nil {
			return nil, err
		}
	}
	for _, j := range jars {
		var err error
		if args, err = appendUnitPath(args, "ReadOnlyPaths", j); err != nil {
			return nil, err
		}
	}
	args = append(args, "--")
	cmd := append([]string{"tmux", "-S", TmuxSocket, "-f", "/dev/null", "new-session", "-d", "-s", Container, "--"}, server.JavaArgs(c, c.JavaBin)...)
	for _, a := range cmd {
		// systemd expands $VAR in a unit's command line, and $$ is a plain $.
		args = append(args, strings.ReplaceAll(a, "$", "$$"))
	}
	return args, nil
}

func appendUnitPath(args []string, key, path string) ([]string, error) {
	// A unit's path list is split on spaces, and takes quotes and escapes.
	if strings.IndexFunc(path, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'\`, r) }) >= 0 {
		return nil, fmt.Errorf("%s: a path for the unit can't hold a space, a quote, or a backslash", path)
	}
	return append(args, "-p", key+"="+path), nil
}

// state is the unit's ActiveState, such as active or inactive. A unit that
// isn't there is inactive.
func (n *Native) state(ctx context.Context) (string, error) {
	out, err := n.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"systemctl", "show", "--property=ActiveState", "--value", Unit}})
	return strings.TrimSpace(string(out)), err
}

func (n *Native) running(ctx context.Context) (bool, error) {
	state, err := n.state(ctx)
	return UnitUp(state), err
}

// UnitUp says whether an ActiveState means the server's process is there.
func UnitUp(state string) bool {
	switch state {
	case "active", "activating", "deactivating", "reloading", "refreshing":
		return true
	}
	return false
}

func (n *Native) latestLog() string {
	return filepath.Join(n.Cfg.ServerDir, "logs", "latest.log")
}

// log is this run's latest.log. Paper moves the last run's aside as it
// starts, so until then there's nothing.
func (n *Native) log(context.Context) ([]byte, error) {
	fi, err := os.Stat(n.latestLog())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	case n.oldLog != nil && os.SameFile(n.oldLog, fi):
		return nil, nil
	}
	return os.ReadFile(n.latestLog())
}

func (n *Native) tail(ctx context.Context) []byte {
	log, _ := n.log(ctx) // the exit is the error worth showing
	lines := bytes.SplitAfter(bytes.TrimRight(log, "\n"), []byte("\n"))
	return bytes.Join(lines[max(0, len(lines)-20):], nil)
}

func (n *Native) logHint() string {
	return n.latestLog()
}

func (n *Native) kill(ctx context.Context) error {
	// The unit's TimeoutStopSec is killGrace, so systemd sends SIGKILL after that.
	_, err := n.Run.Run(ctx, host.Cmd{Args: []string{"systemctl", "stop", Unit}, Sudo: true})
	return err
}

// Stop runs design.md's stop steps that are built so far. warn is the
// countdown for the players, in seconds.
func (n *Native) Stop(ctx context.Context, warn int) error {
	return n.stop(ctx, n, warn)
}
