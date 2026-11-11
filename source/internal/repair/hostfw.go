// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

type port struct {
	num   int
	proto string
}

func (p port) String() string { return fmt.Sprintf("%d/%s", p.num, p.proto) }

func gamePorts(c *config.Config) []port {
	ports := []port{{c.GamePort, "tcp"}}
	if c.Bedrock {
		ports = append(ports, port{c.BedrockPort, "tcp"}, port{c.BedrockPort, "udp"})
	}
	return ports
}

// covers says whether a rule's port spec, such as 25565, 25565/tcp, or
// 25000:26000/tcp in ufw and 25000-26000/tcp in firewalld, lets p in.
func covers(spec string, p port) bool {
	nums, proto, hasProto := strings.Cut(spec, "/")
	if hasProto && proto != p.proto {
		return false
	}
	lo, hi, isRange := strings.Cut(nums, ":")
	if !isRange {
		lo, hi, isRange = strings.Cut(nums, "-")
	}
	if !isRange {
		hi = lo
	}
	from, err1 := strconv.Atoi(lo)
	to, err2 := strconv.Atoi(hi)
	return err1 == nil && err2 == nil && from <= p.num && p.num <= to
}

// hostFirewallCheck only reports. Changing another tool's rules is the
// operator's call. See design.md, repair.
var hostFirewallCheck = Check{
	Name: "ufw and firewalld let players in",
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		var found []Finding
		active := func(unit string) bool {
			out, _ := h.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"systemctl", "is-active", unit}}) // inactive exits 3
			return strings.TrimSpace(string(out)) == "active"
		}
		for _, fw := range []struct {
			unit  string
			allow func(context.Context, *Host) ([]port, error)
			add   func(port) string
		}{
			{"ufw", ufwMissing, func(p port) string { return "sudo ufw allow " + p.String() }},
			{"firewalld", firewalldMissing, func(p port) string {
				return "sudo firewall-cmd --permanent --add-port=" + p.String() + " && sudo firewall-cmd --reload"
			}},
		} {
			if !active(fw.unit) {
				continue
			}
			if !h.Root {
				found = append(found, Finding{Problem: fw.unit + " is active, and its rules can only be read as root; run repair with sudo to check them"})
				continue
			}
			missing, err := fw.allow(ctx, h)
			if err != nil {
				found = append(found, Finding{Problem: "can't read " + fw.unit + "'s rules: " + err.Error()})
				continue
			}
			for _, p := range missing {
				found = append(found, Finding{Problem: fmt.Sprintf("%s is active and doesn't let players in on %s; %s adds it", fw.unit, p, fw.add(p))})
			}
		}
		return found, nil
	},
}

func ufwMissing(ctx context.Context, h *Host) ([]port, error) {
	out, err := h.Run.Run(ctx, host.Cmd{Look: true, Sudo: true, Args: []string{"ufw", "status", "verbose"}})
	if err != nil {
		return nil, err
	}
	var specs []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Default:") && strings.Contains(line, "allow (incoming)") {
			return nil, nil
		}
		f := strings.Fields(line)
		for i, word := range f {
			if word == "ALLOW" && i > 0 {
				specs = append(specs, f[:i]...)
				break
			}
		}
	}
	return uncovered(gamePorts(h.Cfg), specs), nil
}

// firewalldMissing reads the default zone's ports. A service that holds the
// port isn't looked into, so it shows as missing.
func firewalldMissing(ctx context.Context, h *Host) ([]port, error) {
	out, err := h.Run.Run(ctx, host.Cmd{Look: true, Sudo: true, Args: []string{"firewall-cmd", "--list-ports"}})
	if err != nil {
		return nil, err
	}
	return uncovered(gamePorts(h.Cfg), strings.Fields(string(out))), nil
}

func uncovered(ports []port, specs []string) []port {
	var missing []port
	for _, p := range ports {
		ok := false
		for _, s := range specs {
			if covers(s, p) {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, p)
		}
	}
	return missing
}
