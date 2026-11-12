// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package firewall renders the tool's own nftables table and the unit that
// loads it. See design.md, Firewall.
package firewall

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

const (
	Table     = "tt_minecraftd"
	RulesFile = "/etc/tt-minecraftd/firewall.nft"
	UnitName  = "tt-minecraftd-firewall.service"
	UnitFile  = "/etc/systemd/system/" + UnitName
	// rconPort is Minecraft's default. RCON is forced off, so the port only
	// gets a drop in case something opens it anyway.
	rconPort = 25575
)

// lan is what allowFrom's lan means, and what the server can't reach.
// CGNAT is in it, since tailnets live there.
var lan4 = []string{"10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16"}
var lan6 = []string{"fc00::/7", "fe80::/10"}

// Resolvers reads the nameserver lines of a resolv.conf.
func Resolvers(r io.Reader) ([]netip.Addr, error) {
	var addrs []netip.Addr
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		a, err := netip.ParseAddr(f[1])
		if err != nil {
			continue // resolv(5) skips a bad line too
		}
		a = a.WithZone("").Unmap()
		if !slices.Contains(addrs, a) {
			addrs = append(addrs, a)
		}
	}
	return addrs, sc.Err()
}

// Rules is firewall.nft for the config. dns are the resolvers the server may
// still reach on port 53 when they're on the LAN or loopback.
func Rules(c *config.Config, dns []netip.Addr) string {
	var allow4, allow6 []string
	anyone := false
	for _, a := range c.AllowFrom {
		switch a {
		case "any":
			anyone = true
		case "lan":
			allow4 = append(allow4, lan4...)
			allow6 = append(allow6, lan6...)
		default:
			if is4(a) {
				allow4 = append(allow4, a)
			} else {
				allow6 = append(allow6, a)
			}
		}
	}
	var dns4, dns6 []string
	for _, a := range dns {
		if a.Is4() {
			dns4 = append(dns4, a.String())
		} else {
			dns6 = append(dns6, a.String())
		}
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("#!/usr/sbin/nft -f")
	w("# Written by tt-minecraftd from its config. repair writes it again; edits here get lost.")
	w("")
	// Make the table if it's missing, so the delete can't fail, then swap in
	// the new one. nft -f runs the file as one transaction.
	w("table inet %s", Table)
	w("delete table inet %s", Table)
	w("")
	w("table inet %s {", Table)
	set(w, "lan4", "ipv4_addr", lan4, true)
	set(w, "lan6", "ipv6_addr", lan6, true)
	if !anyone {
		set(w, "allow4", "ipv4_addr", allow4, true)
		set(w, "allow6", "ipv6_addr", allow6, true)
	}
	set(w, "dns4", "ipv4_addr", dns4, false)
	set(w, "dns6", "ipv6_addr", dns6, false)
	for _, n := range []string{"flood4", "flood6"} {
		typ := "ipv4_addr"
		if n == "flood6" {
			typ = "ipv6_addr"
		}
		w("\tset %s {\n\t\ttype %s\n\t\tflags dynamic,timeout\n\t\ttimeout 2m\n\t\tsize 65536\n\t}\n", n, typ)
	}
	for _, n := range []string{"in_refused", "in_flood", "in_closed", "out_local", "out_lan", "out_web"} {
		w("\tcounter %s {\n\t}\n", n)
	}

	w("\tchain input {")
	w("\t\ttype filter hook input priority filter; policy accept;")
	w("\t\tiifname \"lo\" accept")
	w("\t\tct state != new accept")
	w("\t\ttcp dport %d jump players", c.GamePort)
	if c.Bedrock {
		w("\t\tmeta l4proto { tcp, udp } th dport %d jump players", c.BedrockPort)
	}
	if rconPort != c.GamePort && !(c.Bedrock && rconPort == c.BedrockPort) {
		w("\t\ttcp dport %d counter name in_closed drop", rconPort)
	}
	// Query listens on the game port over UDP.
	w("\t\tudp dport %d counter name in_closed drop", c.GamePort)
	w("\t}\n")

	w("\tchain players {")
	if !anyone {
		w("\t\tip saddr != @allow4 counter name in_refused drop")
		w("\t\tip6 saddr != @allow6 counter name in_refused drop")
	}
	rate := strconv.Itoa(c.JoinRateLimit)
	w("\t\tupdate @flood4 { ip saddr limit rate over %s/minute burst %s packets } counter name in_flood drop", rate, rate)
	w("\t\tupdate @flood6 { ip6 saddr limit rate over %s/minute burst %s packets } counter name in_flood drop", rate, rate)
	w("\t}\n")

	// A packet the kernel sends by itself, such as IPv6 neighbor discovery,
	// has no socket and so no uid. A != match wouldn't skip it, and it would
	// reach the LAN drop.
	w("\tchain output {")
	w("\t\ttype filter hook output priority filter; policy accept;")
	w("\t\tmeta skuid %s jump server", strconv.Quote(c.RunUser))
	w("\t}\n")

	w("\tchain server {")
	w("\t\tct state established,related accept")
	w("\t\tmeta l4proto { tcp, udp } th dport 53 ip daddr @dns4 accept")
	w("\t\tmeta l4proto { tcp, udp } th dport 53 ip6 daddr @dns6 accept")
	w("\t\toifname \"lo\" counter name out_local goto refuse")
	w("\t\tip daddr @lan4 counter name out_lan goto refuse")
	w("\t\tip6 daddr @lan6 counter name out_lan goto refuse")
	if c.Egress == "web" {
		w("\t\ttcp dport { 80, 443 } accept")
		if c.Bedrock {
			// Geyser's STUN lookup of the home's public address.
			w("\t\tudp dport { 3478, 19302 } accept")
		}
		w("\t\tcounter name out_web goto refuse")
	}
	w("\t}\n")

	w("\tchain refuse {")
	w("\t\tlimit rate 6/minute burst 5 packets log prefix \"tt_minecraftd refused: \" level info")
	w("\t\tdrop")
	w("\t}")
	w("}")
	return b.String()
}

// is4 takes an allowFrom address or range, already checked by config.
func is4(s string) bool {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Addr().Is4()
	}
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

func set(w func(string, ...any), name, typ string, elems []string, ranges bool) {
	w("\tset %s {", name)
	w("\t\ttype %s", typ)
	if ranges {
		w("\t\tflags interval")
		w("\t\tauto-merge")
	}
	if len(elems) > 0 {
		w("\t\telements = { %s }", strings.Join(elems, ", "))
	}
	w("\t}\n")
}

// Unit is tt-minecraftd-firewall.service. It loads the rules before the
// network comes up, and again after nftables.service starts or reloads,
// since Debian's nftables.conf flushes every table.
func Unit() string {
	return `[Unit]
Description=tt-minecraftd firewall rules
Documentation=https://github.com/t00mietum/tt-minecraft-safeserver
DefaultDependencies=no
Wants=network-pre.target
Before=network-pre.target shutdown.target
After=nftables.service
Conflicts=shutdown.target
PartOf=nftables.service
ReloadPropagatedFrom=nftables.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f ` + RulesFile + `
ExecReload=/usr/sbin/nft -f ` + RulesFile + `
ExecStop=-/usr/sbin/nft delete table inet ` + Table + `

[Install]
WantedBy=sysinit.target nftables.service
`
}

// Loaded says whether the table is in the kernel. Listing needs root.
func Loaded(ctx context.Context, r host.Runner) (bool, error) {
	out, err := r.Run(ctx, host.Cmd{Look: true, Sudo: true, Args: []string{"nft", "list", "tables", "inet"}})
	if err != nil {
		return false, fmt.Errorf("can't list the nftables tables: %w", err)
	}
	for line := range strings.Lines(string(out)) {
		if strings.TrimSpace(line) == "table inet "+Table {
			return true, nil
		}
	}
	return false, nil
}

// Counters gives the packets each of the table's named counters has dropped,
// by name. It's empty when the table isn't loaded. Reading them needs root,
// and it doesn't use sudo, so the caller checks.
func Counters(ctx context.Context, r host.Runner) (map[string]uint64, error) {
	// Listing every inet counter, rather than the table's, has nft answer
	// the same way when the table is gone.
	out, err := r.Run(ctx, host.Cmd{Look: true, Args: []string{"nft", "-j", "list", "counters", "inet"}})
	if err != nil {
		return nil, fmt.Errorf("can't list the nftables counters: %w", err)
	}
	var list struct {
		Nftables []struct {
			Counter *struct {
				Family  string `json:"family"`
				Table   string `json:"table"`
				Name    string `json:"name"`
				Packets uint64 `json:"packets"`
			} `json:"counter"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("can't read nft's counter list: %w", err)
	}
	counts := map[string]uint64{}
	for _, o := range list.Nftables {
		if c := o.Counter; c != nil && c.Family == "inet" && c.Table == Table {
			counts[c.Name] = c.Packets
		}
	}
	return counts, nil
}
