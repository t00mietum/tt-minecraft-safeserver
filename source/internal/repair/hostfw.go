// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

type port struct {
	num   int
	proto string
	// deny is the ufw rule that blocks it ahead of any allow, if one does.
	deny string
}

func (p port) String() string { return fmt.Sprintf("%d/%s", p.num, p.proto) }

func gamePorts(c *config.Config) []port {
	ports := []port{{num: c.GamePort, proto: "tcp"}}
	if c.Bedrock {
		ports = append(ports, port{num: c.BedrockPort, proto: "tcp"}, port{num: c.BedrockPort, proto: "udp"})
	}
	return ports
}

// covers says whether a rule's port spec lets p in. ufw gives 25565,
// 25565/tcp, 25000:26000/tcp, a list such as 19132,25565/tcp, and Anywhere
// for a rule with no port. firewalld gives 25000-26000/tcp.
func covers(spec string, p port) bool {
	nums, proto, hasProto := strings.Cut(spec, "/")
	if hasProto && proto != p.proto {
		return false
	}
	if nums == "Anywhere" {
		return true
	}
	for one := range strings.SplitSeq(nums, ",") {
		lo, hi, isRange := strings.Cut(one, ":")
		if !isRange {
			lo, hi, isRange = strings.Cut(one, "-")
		}
		if !isRange {
			hi = lo
		}
		from, err1 := strconv.Atoi(lo)
		to, err2 := strconv.Atoi(hi)
		if err1 == nil && err2 == nil && from <= p.num && p.num <= to {
			return true
		}
	}
	return false
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
				if p.deny != "" {
					found = append(found, Finding{Problem: fmt.Sprintf("%s is active and its rule %q blocks players on %s, ahead of any rule that lets them in", fw.unit, p.deny, p)})
					continue
				}
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
	type rule struct {
		to   []string
		deny string
	}
	var rules []rule
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Default:") && strings.Contains(line, "allow (incoming)") {
			return nil, nil
		}
		// verbose always names the direction; ALLOW OUT and ALLOW FWD don't let players in.
		// LIMIT IN does, and only refuses an address that connects 6 times in 30 s.
		// A DENY or REJECT counts only from Anywhere, as the From column of an
		// allow isn't read either.
		f := strings.Fields(line)
		for i, word := range f {
			if i == 0 || i+1 >= len(f) || f[i+1] != "IN" {
				continue
			}
			switch word {
			case "ALLOW", "LIMIT":
				rules = append(rules, rule{to: f[:i]})
			case "DENY", "REJECT":
				if i+2 < len(f) && f[i+2] == "Anywhere" {
					rules = append(rules, rule{to: f[:i], deny: strings.Join(f, " ")})
				}
			}
			break
		}
	}
	// ufw lists its rules in the order it checks them, and the first match
	// wins. The IPv6 ones come last, so they decide only a port no IPv4
	// rule names.
	var missing []port
	for _, p := range gamePorts(h.Cfg) {
		i := slices.IndexFunc(rules, func(r rule) bool {
			return slices.ContainsFunc(r.to, func(s string) bool { return covers(s, p) })
		})
		switch {
		case i < 0:
			missing = append(missing, p)
		case rules[i].deny == "":
		case strings.TrimSuffix(strings.Join(rules[i].to, " "), " (v6)") == p.String():
			// ufw allow replaces a rule that differs only in its action.
			missing = append(missing, p)
		default:
			p.deny = rules[i].deny
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// firewalldMissing reads the default zone's ports. A service that opens the
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
		if !slices.ContainsFunc(specs, func(s string) bool { return covers(s, p) }) {
			missing = append(missing, p)
		}
	}
	return missing
}
