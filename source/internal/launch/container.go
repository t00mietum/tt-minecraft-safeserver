// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package launch starts and stops the server in a container or in native mode.
package launch

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/tree"
)

// Container is the name and the label of the one container this tool runs.
const Container = "tt-minecraftd"

// Wrapper is the shell line the server starts under in a container. The
// server runs with umask 0007, so new files stay out of reach of others.
// status also knows the container by it.
const Wrapper = `umask 0007 && exec "$@"`

// pidsLimit leaves room for Paper's thread pools, which count as pids.
const pidsLimit = 2048

// killGrace is how long SIGTERM gets before SIGKILL, per design.md.
const killGrace = 30

// Boxed runs the server in a Podman or Docker container, through sudo. See
// design.md, Run modes.
type Boxed struct {
	Kit
	// IDs gives the uid and gid for --user, since the image doesn't know the
	// host's names.
	IDs func(userName, groupName string) (uid, gid string, err error)
	// Runtime is podman or docker, or runMode when empty. Stop, console, and
	// cmd move it to the runtime that holds the container.
	Runtime string
}

// Runtimes are the container runtimes, in the order setup prefers them.
var Runtimes = []string{"podman", "docker"}

// Installed says whether a program is on PATH. Tests replace it.
var Installed = func(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// StartMode holds start's options.
type StartMode struct {
	Debug      bool
	Foreground bool
}

// HostIDs looks the names up on the host.
func HostIDs(userName, groupName string) (string, string, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return "", "", err
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return "", "", err
	}
	return u.Uid, g.Gid, nil
}

// ContainerState is the container's state in one runtime, such as running or
// exited, or "" when there's no container.
func ContainerState(ctx context.Context, r host.Runner, runtime string) (string, error) {
	out, err := r.Run(ctx, host.Cmd{Look: true, Sudo: true, Args: PsArgs(runtime)})
	return strings.TrimSpace(string(out)), err
}

// PsArgs asks a runtime for the container's state.
func PsArgs(runtime string) []string {
	return []string{runtime, "ps", "--all", "--filter", "name=^" + Container + "$", "--filter", "label=" + Container, "--format", "{{.State}}"}
}

// FindContainer gives the runtime that holds the container, and its state.
// runMode's runtime is asked first, then any other that's installed, since
// the container may be from before a runMode change. A running container
// wins over a stopped one. Both are "" when there's none.
func FindContainer(ctx context.Context, r host.Runner, runMode string) (runtime, state string, err error) {
	for _, rt := range Runtimes {
		if rt != runMode {
			continue
		}
		if state, err = ContainerState(ctx, r, rt); err != nil || state == "running" {
			return rt, state, err
		}
		if state != "" {
			runtime = rt
		}
	}
	for _, rt := range Runtimes {
		if rt == runMode || !Installed(rt) {
			continue
		}
		// Only runMode's runtime has to answer. Docker's daemon may well be
		// down on a host that moved to Podman.
		other, err := ContainerState(ctx, r, rt)
		switch {
		case err != nil || other == "":
		case other == "running":
			return rt, other, nil
		case runtime == "":
			runtime, state = rt, other
		}
	}
	return runtime, state, nil
}

// locate moves to the runtime that holds the container, when it's not runMode's.
func (d *Boxed) locate(ctx context.Context) error {
	if err := d.sudoUp(ctx); err != nil {
		return err
	}
	rt, _, err := FindContainer(ctx, d.Run, d.runtime())
	if err != nil {
		return err
	}
	if rt != "" && rt != d.runtime() {
		d.say("The container is under %s, not %s as runMode says.", rt, d.runtime())
		d.Runtime = rt
	}
	if _, ok := d.Console.(AttachConsole); ok || d.Console == nil {
		d.Console = AttachConsole{Runtime: d.runtime(), Root: d.Root, DryRun: d.DryRun, Out: d.Out}
	}
	return nil
}

func (d *Boxed) runtime() string {
	if d.Runtime == "" {
		return d.Cfg.RunMode
	}
	return d.Runtime
}

// box is one runtime command, through sudo.
func (d *Boxed) box(look bool, args ...string) host.Cmd {
	return host.Cmd{Look: look, Sudo: true, Args: append([]string{d.runtime()}, args...)}
}

func (d *Boxed) state(ctx context.Context) (string, error) {
	return ContainerState(ctx, d.Run, d.runtime())
}

// Start runs design.md's start steps that are built so far.
func (d *Boxed) Start(ctx context.Context, mode StartMode) error {
	if err := d.refuseOthers(ctx); err != nil {
		return err
	}
	if !mode.Foreground {
		if err := d.sudoUp(ctx); err != nil {
			return err
		}
	}
	if err := d.checkSettings(); err != nil {
		return err
	}
	if err := d.checkPlugins(); err != nil {
		return err
	}
	// The firewall and snapshot steps come with their own items.
	args, err := d.runArgs(mode)
	if err != nil {
		return err
	}
	state, err := d.state(ctx)
	if err != nil {
		return err
	}
	switch state {
	case "":
	case "running":
		return errors.New("the server's container is already running")
	default:
		if _, err := d.Run.Run(ctx, d.box(false, "rm", Container)); err != nil {
			return err
		}
	}
	warns, err := tree.ToServer(ctx, d.Run, d.Cfg)
	d.warn(warns)
	if err != nil {
		return err
	}

	if mode.Debug {
		d.say("Starting with the console attached. Type stop to stop the server.")
		c := d.box(false, args...)
		c.Tty = true
		_, err := d.Run.Run(ctx, c)
		return errors.Join(err, d.handBack(ctx))
	}
	if _, err := d.Run.Run(ctx, d.box(false, args...)); err != nil {
		return err
	}
	if d.DryRun {
		return nil
	}
	if err := d.waitLoaded(ctx, d); err != nil {
		return err
	}
	if !mode.Foreground {
		return nil
	}
	if _, err := d.Run.Run(ctx, d.box(true, "wait", Container)); err != nil {
		return err
	}
	return d.handBack(ctx)
}

func (d *Boxed) running(ctx context.Context) (bool, error) {
	state, err := d.state(ctx)
	return state == "running", err
}

func (d *Boxed) log(ctx context.Context) ([]byte, error) {
	return d.Run.Run(ctx, d.box(true, "logs", Container))
}

func (d *Boxed) tail(ctx context.Context) []byte {
	tail, _ := d.Run.Run(ctx, d.box(true, "logs", "--tail", "20", Container)) // the exit is the error worth showing
	return tail
}

func (d *Boxed) logHint() string {
	return "sudo " + d.runtime() + " logs " + Container
}

func (d *Boxed) kill(ctx context.Context) error {
	_, err := d.Run.Run(ctx, d.box(false, "stop", "--time", strconv.Itoa(killGrace), Container))
	return err
}

// runArgs builds the run command, after the runtime's name. Podman and Docker
// get the same flags. See design.md, Run modes.
func (d *Boxed) runArgs(mode StartMode) ([]string, error) {
	c := d.Cfg
	uid, gid, err := d.IDs(c.RunUser, c.RunGroup)
	if err != nil {
		return nil, fmt.Errorf("service account: %w", err)
	}
	memCap := strconv.FormatInt(c.MemCap, 10)
	args := []string{"run"}
	if !mode.Debug {
		args = append(args, "--detach")
	}
	args = append(args,
		"--name", Container, "--label", Container,
		"--user", uid+":"+gid,
		"--network", "host",
		"--read-only", "--tmpfs", "/tmp:rw,exec,nosuid,nodev",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--pids-limit", strconv.Itoa(pidsLimit),
		"--memory", memCap, "--memory-swap", memCap,
		"--oom-score-adj", "1000",
		"--init", "--interactive", "--tty",
		"--workdir", c.ServerDir,
	)
	for _, t := range c.OwnerTrees {
		real, err := filepath.EvalSymlinks(t)
		if err != nil {
			// tree.ToServer warns about a missing tree.
			continue
		}
		if args, err = appendMount(args, real, false); err != nil {
			return nil, err
		}
	}
	jars, err := LockedJars(c.ServerDir)
	if err != nil {
		return nil, err
	}
	for _, j := range jars {
		if args, err = appendMount(args, j, true); err != nil {
			return nil, err
		}
	}
	args = append(args, c.Image, "sh", "-c", Wrapper, "sh")
	return append(args, server.JavaArgs(c, "java")...), nil
}

func appendMount(args []string, path string, readOnly bool) ([]string, error) {
	// --mount is comma separated, and a quote would need escaping too.
	if strings.ContainsAny(path, `,"`) {
		return nil, fmt.Errorf("%s: a path for the container can't hold a comma or a quote", path)
	}
	m := "type=bind,src=" + path + ",dst=" + path
	if readOnly {
		m += ",readonly"
	}
	return append(args, "--mount", m), nil
}

// LockedJars gives paper.jar and the jars plugins.Jars checks, after links, so
// each can be mounted read-only over itself.
func LockedJars(serverDir string) ([]string, error) {
	paper, err := filepath.EvalSymlinks(filepath.Join(serverDir, "paper.jar"))
	if err != nil {
		return nil, fmt.Errorf("no paper.jar in %s: %w", serverDir, err)
	}
	found, err := plugins.Jars(serverDir)
	if err != nil {
		return nil, err
	}
	jars := make([]string, 0, len(found))
	jars = append(jars, paper)
	for _, j := range found {
		real, err := filepath.EvalSymlinks(j.File)
		if err != nil {
			return nil, err
		}
		if real != paper {
			jars = append(jars, real)
		}
	}
	return jars, nil
}

// Stop runs design.md's stop steps that are built so far. warn is the
// countdown for the players, in seconds.
func (d *Boxed) Stop(ctx context.Context, warn int) error {
	if err := d.locate(ctx); err != nil {
		return err
	}
	return d.stop(ctx, d, warn)
}

// OpenConsole is console. Ctrl-C is the detach key, so it can't reach the
// server's terminal and stop it by accident.
func (d *Boxed) OpenConsole(ctx context.Context) error {
	if err := d.locate(ctx); err != nil {
		return err
	}
	if err := d.mustRun(ctx, d); err != nil {
		return err
	}
	d.say(consoleHello)
	c := d.box(false, "attach", "--sig-proxy=false", "--detach-keys=ctrl-c", Container)
	c.Tty = true
	return d.session(ctx, d, c, attachDetached)
}

// attachDetached is docker attach's exit after the detach keys, which is 1.
// Podman's is 0.
func attachDetached(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// SendCmd is cmd: one console line, and what the server says back.
func (d *Boxed) SendCmd(ctx context.Context, line string) error {
	if err := d.locate(ctx); err != nil {
		return err
	}
	return d.command(ctx, d, line)
}
