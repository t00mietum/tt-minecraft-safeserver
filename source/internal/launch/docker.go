// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package launch starts and stops the server in Docker or native mode.
package launch

import (
	"context"
	"errors"
	"fmt"
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

// pidsLimit leaves room for Paper's thread pools, which count as pids.
const pidsLimit = 2048

// killGrace is how long SIGTERM gets before SIGKILL, per design.md.
const killGrace = 30

// Docker runs the server in a container. See design.md, Run modes.
type Docker struct {
	Kit
	// IDs gives the uid and gid for --user, since the image doesn't know the
	// host's names.
	IDs func(userName, groupName string) (uid, gid string, err error)
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

// ContainerState is the container's state, such as running or exited, or ""
// when there's no container.
func ContainerState(ctx context.Context, r host.Runner) (string, error) {
	out, err := r.Run(ctx, host.Cmd{Look: true, Args: []string{
		"docker", "ps", "--all", "--filter", "name=^" + Container + "$", "--filter", "label=" + Container, "--format", "{{.State}}",
	}})
	return strings.TrimSpace(string(out)), err
}

func (d *Docker) state(ctx context.Context) (string, error) {
	return ContainerState(ctx, d.Run)
}

// Start runs design.md's start steps that are built so far.
func (d *Docker) Start(ctx context.Context, mode StartMode) error {
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
		if _, err := d.Run.Run(ctx, host.Cmd{Args: []string{"docker", "rm", Container}}); err != nil {
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
		_, err := d.Run.Run(ctx, host.Cmd{Args: args, Tty: true})
		return errors.Join(err, d.handBack(ctx))
	}
	if _, err := d.Run.Run(ctx, host.Cmd{Args: args}); err != nil {
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
	if _, err := d.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"docker", "wait", Container}}); err != nil {
		return err
	}
	return d.handBack(ctx)
}

func (d *Docker) running(ctx context.Context) (bool, error) {
	state, err := d.state(ctx)
	return state == "running", err
}

func (d *Docker) log(ctx context.Context) ([]byte, error) {
	return d.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"docker", "logs", Container}})
}

func (d *Docker) tail(ctx context.Context) []byte {
	tail, _ := d.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"docker", "logs", "--tail", "20", Container}}) // the exit is the error worth showing
	return tail
}

func (d *Docker) logHint() string {
	return "docker logs " + Container
}

func (d *Docker) kill(ctx context.Context) error {
	_, err := d.Run.Run(ctx, host.Cmd{Args: []string{"docker", "stop", "--time", strconv.Itoa(killGrace), Container}})
	return err
}

// runArgs builds the docker run command. See design.md, Run modes.
func (d *Docker) runArgs(mode StartMode) ([]string, error) {
	c := d.Cfg
	uid, gid, err := d.IDs(c.RunUser, c.RunGroup)
	if err != nil {
		return nil, fmt.Errorf("service account: %w", err)
	}
	memCap := strconv.FormatInt(c.MemCap, 10)
	args := []string{"docker", "run"}
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
	jars, err := lockedJars(c.ServerDir)
	if err != nil {
		return nil, err
	}
	for _, j := range jars {
		if args, err = appendMount(args, j, true); err != nil {
			return nil, err
		}
	}
	// The server runs with umask 0007, so new files stay out of reach of others.
	args = append(args, c.DockerImage, "sh", "-c", `umask 0007 && exec "$@"`, "sh")
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

// lockedJars gives paper.jar and the jars plugins.Jars checks, after links, so
// each can be mounted read-only over itself.
func lockedJars(serverDir string) ([]string, error) {
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
func (d *Docker) Stop(ctx context.Context, warn int) error {
	return d.stop(ctx, d, warn)
}
