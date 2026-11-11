// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
)

var dockerCheck = Check{
	Name:    "Docker image and container",
	Applies: func(c *config.Config) bool { return c.RunMode != "native" },
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		image := h.Cfg.DockerImage
		var found []Finding
		if _, err := h.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"docker", "image", "inspect", "--format", "{{.Id}}", image}}); err != nil {
			found = append(found, Finding{
				Problem: "image " + image + " isn't there, or docker can't be asked: " + err.Error(),
				Fixed:   "image " + image + " pulled",
				Fix:     h.fix("docker", "pull", image),
			})
		}
		state, err := launch.ContainerState(ctx, h.Run)
		if err != nil {
			return found, nil // the image finding already says docker can't be asked
		}
		if state != "" && state != "running" {
			found = append(found, Finding{
				Problem: "an old container, " + launch.Container + ", is still there, " + state,
				Fixed:   "old container " + launch.Container + " removed",
				Fix:     h.fix("docker", "rm", launch.Container),
			})
		}
		return found, nil
	},
}

var datasetCheck = Check{
	Name:    "ZFS dataset",
	Applies: func(c *config.Config) bool { return c.ZfsDataset != "" },
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		ds := h.Cfg.ZfsDataset
		out, err := h.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"zfs", "get", "-H", "-o", "value", "mounted", "--", ds}})
		if err != nil {
			return []Finding{{Problem: "dataset " + ds + " doesn't exist, or zfs can't be asked: " + err.Error()}}, nil
		}
		switch strings.TrimSpace(string(out)) {
		case "yes":
			return nil, nil
		case "no":
			return []Finding{{
				Problem: "dataset " + ds + " isn't mounted",
				Fixed:   "dataset " + ds + " mounted",
				Fix:     h.fix("zfs", "mount", "--", ds),
			}}, nil
		}
		return []Finding{{Problem: "dataset " + ds + " isn't a file system that can be mounted"}}, nil
	},
}

// othersCheck names Paper servers running as anyone but runUser. Both modes
// run the server under runUser's uid on the host, the container included.
var othersCheck = Check{
	Name: "no other Paper servers",
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		ps, err := h.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"ps", "-eo", "uid=,pid=,args="}})
		if err != nil {
			return nil, err
		}
		a, err := h.accounts()
		if err != nil {
			return nil, err
		}
		ours := ""
		if u, ok := a.user(h.Cfg.RunUser); ok {
			ours = strconv.Itoa(u.uid)
		}
		var found []Finding
		for _, p := range server.PaperProcs(ps) {
			if p.User == ours {
				continue
			}
			name := p.User
			if uid, err := strconv.Atoi(p.User); err == nil {
				name = a.userName(uid)
			}
			found = append(found, Finding{Problem: "a Paper server this tool didn't start is running, pid " + p.PID + " as " + name + "; it's left alone, and start refuses until it's stopped"})
		}
		return found, nil
	},
}
