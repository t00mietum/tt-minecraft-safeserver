// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package status

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
)

// proc is one line of PsArgs.
type proc struct {
	uid, pid, ppid string
	age            time.Duration
	args           string
}

// PsArgs lists the processes, which needs no sudo.
var PsArgs = []string{"ps", "-eo", "uid=,pid=,ppid=,etimes=,args="}

var procLine = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(.*)$`)

func readProcs(ps []byte) []proc {
	lines := strings.Split(string(ps), "\n")
	procs := make([]proc, 0, len(lines))
	for _, l := range lines {
		m := procLine.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		secs, err := strconv.ParseInt(m[4], 10, 64)
		if err != nil {
			continue
		}
		procs = append(procs, proc{uid: m[1], pid: m[2], ppid: m[3], age: time.Duration(secs) * time.Second, args: m[5]})
	}
	return procs
}

// sighting is the server this tool runs, if any, and the other Paper servers.
type sighting struct {
	mode   string // podman, docker, or native, or "" when not running
	proc   proc
	others []proc
}

// find looks in the process table, which needs no sudo, unlike asking a
// rootful runtime. The server's cgroup gives its mode, whatever runMode says.
func (p *Probe) find(ctx context.Context) (sighting, error) {
	out, err := p.Run.Run(ctx, host.Cmd{Look: true, Args: PsArgs})
	if err != nil {
		return sighting{}, err
	}
	return sight(readProcs(out), p.ReadFile), nil
}

func sight(procs []proc, readFile func(string) ([]byte, error)) sighting {
	byPID := make(map[string]proc, len(procs))
	for _, pr := range procs {
		byPID[pr.pid] = pr
	}
	var s sighting
	for _, pr := range procs {
		if !server.IsPaper(pr.args) {
			continue
		}
		mode := ours(pr, byPID, readFile)
		if mode == "" || s.mode != "" {
			s.others = append(s.others, pr)
			continue
		}
		s.mode, s.proc = mode, pr
	}
	return s
}

// Other is a Paper server this tool didn't start. UID is as ps gave it.
type Other struct {
	UID, PID string
}

// Others picks the Paper servers this tool didn't start out of the output of
// PsArgs, told apart by cgroup and wrapper as status does. A second server
// that looks like ours counts too, since start runs only one. readFile reads
// /proc.
func Others(ps []byte, readFile func(string) ([]byte, error)) []Other {
	var found []Other
	for _, o := range sight(readProcs(ps), readFile).others {
		found = append(found, Other{UID: o.uid, PID: o.pid})
	}
	return found
}

// Mode is the mode the tool's server runs in, whatever runMode says, or ""
// when it isn't running.
func (p *Probe) Mode(ctx context.Context) (string, error) {
	s, err := p.find(ctx)
	return s.mode, err
}

// ours gives the mode of a Paper process this tool started, or "". Its uid
// doesn't tell, since a Paper started by hand as runUser has the same one.
func ours(pr proc, byPID map[string]proc, readFile func(string) ([]byte, error)) string {
	cg, err := readFile("/proc/" + pr.pid + "/cgroup")
	if err != nil {
		return ""
	}
	mode := cgroupMode(cg)
	if mode == "" || mode == "native" {
		return mode
	}
	// Some other container may run Paper. Ours runs under the container's
	// init, which got the wrapper line.
	if parent, ok := byPID[pr.ppid]; ok && strings.Contains(parent.args, " sh -c "+launch.Wrapper+" sh ") {
		return mode
	}
	return ""
}

var (
	containerScope = regexp.MustCompile(`^(libpod|docker)-[0-9a-f]{64}(\.scope)?$`)
	containerID    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// cgroupMode reads /proc/<pid>/cgroup. Podman's scope is libpod-<id>.scope,
// with crun's container cgroup under it, and Docker's is docker-<id>.scope,
// or docker/<id> with the cgroupfs driver.
func cgroupMode(cg []byte) string {
	for _, l := range strings.Split(string(cg), "\n") {
		_, after, _ := strings.Cut(l, ":")
		_, path, ok := strings.Cut(after, ":")
		if !ok {
			continue
		}
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if m := containerScope.FindStringSubmatch(part); m != nil {
				if m[1] == "libpod" {
					return "podman"
				}
				return "docker"
			}
			if part == "docker" && i+1 < len(parts) && containerID.MatchString(parts[i+1]) {
				return "docker"
			}
		}
		if parts[len(parts)-1] == launch.Unit {
			return "native"
		}
	}
	return ""
}
