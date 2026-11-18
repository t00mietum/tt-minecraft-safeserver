// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package firewall renders the tool's own nftables table and the unit that
// loads it. See design.md, Firewall.
package firewall

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

const (
	// Table is the tool's own nftables table. The rest are where its rules
	// and their unit go.
	Table     = "tt_minecraftd"
	RulesFile = "/etc/tt-minecraftd/firewall.nft"
	UnitName  = "tt-minecraftd-firewall.service"
	UnitFile  = "/etc/systemd/system/" + UnitName
	// rconPort is Minecraft's default. RCON is forced off, so the port only
	// gets a drop in case something opens it anyway.
	rconPort = 25575
	// discoveryPort is where Bedrock 26.60 and later broadcast to find LAN
	// games, from any port of their own.
	discoveryPort = 7551
)

// lan4 and lan6 are what allowFrom's lan means, and what the server can't
// reach. CGNAT is in it, since tailnets live there. The host's own IPv6
// subnets are added to the server's side only, from Local.Nets.
var (
	lan4 = []string{"10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16"}
	lan6 = []string{"fc00::/7", "fe80::/10"}
)

// Local is what the rules take from the host rather than the config.
type Local struct {
	// DNS are the resolvers the server may still reach on port 53 when
	// they're on the LAN or loopback.
	DNS []netip.Addr
	// Nets are the host's on-link IPv6 prefixes. A home LAN's router, NAS,
	// and printers have global addresses there.
	Nets []netip.Prefix
}

// Files are where ReadLocal and Matches look. Empty means the real ones.
type Files struct {
	Rules, ResolvConf, Routes string
}

func (f Files) orReal() Files {
	if f.Rules == "" {
		f.Rules = RulesFile
	}
	if f.ResolvConf == "" {
		f.ResolvConf = "/etc/resolv.conf"
	}
	if f.Routes == "" {
		f.Routes = "/proc/net/ipv6_route"
	}
	return f
}

// ReadLocal reads the resolvers and the on-link prefixes. With IPv6 off at
// boot there's no route file, and so no prefixes.
func ReadLocal(f Files) (Local, error) {
	f = f.orReal()
	conf, err := os.ReadFile(f.ResolvConf) // #nosec G304 -- resolv.conf
	if err != nil {
		return Local{}, err
	}
	dns, err := Resolvers(bytes.NewReader(conf))
	if err != nil {
		return Local{}, err
	}
	routes, err := os.ReadFile(f.Routes) // #nosec G304 -- the kernel's route list
	if errors.Is(err, fs.ErrNotExist) {
		return Local{DNS: dns}, nil
	}
	if err != nil {
		return Local{}, err
	}
	nets, err := OnLink(bytes.NewReader(routes))
	return Local{DNS: dns, Nets: nets}, err
}

// Matches says whether the rules file is the one the config and the host
// give. A prefix the file has and the host doesn't is fine, since it only
// blocks more: at boot the router's announcement can come after the server
// starts. A prefix the host has and the file lacks isn't. A missing file
// doesn't match.
func Matches(c *config.Config, f Files) (bool, error) {
	f = f.orReal()
	have, err := os.ReadFile(f.Rules) // #nosec G304 -- the tool's own file
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	l, err := ReadLocal(f)
	if err != nil {
		return false, err
	}
	l.Nets = append(l.Nets, fileNets(string(have))...)
	return string(have) == Rules(c, l), nil
}

// fileNets are the prefixes in a rules file's lan6 set past the fixed ones.
func fileNets(text string) []netip.Prefix {
	_, rest, ok := strings.Cut(text, "\tset lan6 {\n")
	if !ok {
		return nil
	}
	body, _, _ := strings.Cut(rest, "\t}\n")
	_, elems, ok := strings.Cut(body, "elements = { ")
	if !ok {
		return nil
	}
	elems, _, _ = strings.Cut(elems, " }")
	var nets []netip.Prefix
	for e := range strings.SplitSeq(elems, ", ") {
		if p, err := netip.ParsePrefix(e); err == nil {
			nets = append(nets, p)
		}
	}
	return nets
}

// Route flags from the kernel's route.h and ipv6_route.h.
const (
	rtfUp      = 0x1
	rtfGateway = 0x2
	rtfReject  = 0x200
	rtfAnycast = 0x100000
	rtfCache   = 0x1000000
	rtfLocal   = 0x80000000
)

// minOnLink is the widest on-link route taken as the host's own subnet. A
// VPN can put a large part of the internet, or all of it, on its device
// with no gateway, and the server would lose IPv6 if those counted.
const minOnLink = 32

// OnLink reads /proc/net/ipv6_route for the host's on-link prefixes: routes
// with no gateway, not on loopback, and not already in the private ranges.
func OnLink(r io.Reader) ([]netip.Prefix, error) {
	var nets []netip.Prefix
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		// dest, dest len, src, src len, next hop, metric, refs, use, flags, device
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[9] == "lo" {
			continue
		}
		flags, err := strconv.ParseUint(f[8], 16, 32)
		if err != nil || flags&rtfUp == 0 || flags&(rtfGateway|rtfReject|rtfAnycast|rtfCache|rtfLocal) != 0 || strings.Trim(f[4], "0") != "" {
			continue
		}
		bits, err := strconv.ParseUint(f[1], 16, 8)
		if err != nil || bits < minOnLink || bits > 128 {
			continue
		}
		raw, err := hex.DecodeString(f[0])
		if err != nil || len(raw) != 16 {
			continue
		}
		p, err := netip.AddrFrom16([16]byte(raw)).Prefix(int(bits))
		if err == nil {
			nets = append(nets, p)
		}
	}
	return tidy(nets), sc.Err()
}

// tidy keeps the global unicast prefixes that the fixed ranges and each
// other don't already cover, in order, so the file reads the same each time.
func tidy(nets []netip.Prefix) []netip.Prefix {
	fixed := make([]netip.Prefix, 0, len(lan6))
	for _, s := range lan6 {
		fixed = append(fixed, netip.MustParsePrefix(s))
	}
	// within says whether one of by holds p. A prefix isn't within itself.
	within := func(p netip.Prefix, by []netip.Prefix) bool {
		return slices.ContainsFunc(by, func(q netip.Prefix) bool { return q != p && q.Bits() <= p.Bits() && q.Contains(p.Addr()) })
	}
	var keep []netip.Prefix
	for _, p := range nets {
		p = p.Masked()
		if !p.IsValid() || !p.Addr().Is6() || p.Addr().Is4In6() || !p.Addr().IsGlobalUnicast() || slices.Contains(fixed, p) || within(p, fixed) || slices.Contains(keep, p) {
			continue
		}
		keep = append(keep, p)
	}
	var out []netip.Prefix
	for _, p := range keep {
		if !within(p, keep) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	return out
}

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

// Rules is firewall.nft for the config and the host.
func Rules(c *config.Config, l Local) string {
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
	for _, a := range l.DNS {
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
	var mine []string
	for _, p := range tidy(l.Nets) {
		mine = append(mine, p.String())
	}
	set(w, "lan6", "ipv6_addr", append(slices.Clone(lan6), mine...), true)
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
	if c.Bedrock {
		// Who broadcast for LAN games lately. Clients repeat it every 2 s.
		w("\tset seek4 {\n\t\ttype ipv4_addr . inet_service\n\t\tflags dynamic,timeout\n\t\ttimeout 10s\n\t\tsize 4096\n\t}\n")
	}
	for _, n := range []string{"in_refused", "in_flood", "in_closed", "out_local", "out_lan", "out_web"} {
		w("\tcounter %s {\n\t}\n", n)
	}

	w("\tchain input {")
	w("\t\ttype filter hook input priority filter; policy accept;")
	w("\t\tiifname \"lo\" accept")
	if c.Bedrock {
		from := "ip saddr @lan4"
		if !anyone {
			from += " ip saddr @allow4"
		}
		w("\t\tmeta pkttype broadcast udp dport %d %s update @seek4 { ip saddr . udp sport }", discoveryPort, from)
	}
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
	// Geyser's NetherNet sends to its own port over loopback, and STUN gets
	// no answer when those are dropped.
	w("\t\toifname \"lo\" tcp dport %d accept", c.GamePort)
	if c.Bedrock {
		w("\t\toifname \"lo\" meta l4proto { tcp, udp } th dport %d accept", c.BedrockPort)
	}
	w("\t\toifname \"lo\" counter name out_local goto refuse")
	if c.Bedrock {
		// Conntrack keys a broadcast on the broadcast address, so the answer
		// to it is a new packet. Only the address and port that asked get one.
		w("\t\tudp sport %d ip daddr . udp dport @seek4 accept", discoveryPort)
	}
	w("\t\tip daddr @lan4 counter name out_lan goto refuse")
	w("\t\tip6 daddr @lan6 counter name out_lan goto refuse")
	// Multicast and broadcast reach the LAN too, such as SSDP to the router.
	w("\t\tip daddr { 224.0.0.0/4, 255.255.255.255 } counter name out_lan goto refuse")
	w("\t\tip6 daddr ff00::/8 counter name out_lan goto refuse")
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
