// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package firewall

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// nftCheck has nft check the rules in a network namespace of their own, where
// that works without root. It reports whether it ran.
func nftCheck(t *testing.T, rules string) bool {
	t.Helper()
	if _, err := exec.LookPath("unshare"); err != nil {
		return false
	}
	nft := "/usr/sbin/nft"
	if _, err := os.Stat(nft); err != nil {
		return false
	}
	if exec.Command("unshare", "-rn", nft, "list", "ruleset").Run() != nil {
		return false
	}
	path := filepath.Join(t.TempDir(), "firewall.nft")
	if err := os.WriteFile(path, []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- fixed binary, test file
	if out, err := exec.Command("unshare", "-rn", nft, "-c", "-f", path).CombinedOutput(); err != nil {
		t.Errorf("nft -c: %v\n%s\n%s", err, out, rules)
	}
	return true
}

func TestRules(t *testing.T) {
	defer testid.Line(t, "df66c")
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dns := []netip.Addr{netip.MustParseAddr("127.0.0.53"), netip.MustParseAddr("192.168.1.1"), netip.MustParseAddr("fd00::1")}
	cases := []struct {
		name    string
		allow   []string
		egress  string
		bedrock bool
		has     []string
		hasNot  []string
	}{
		{"defaults", []string{"lan"}, "web", true,
			[]string{
				"tcp dport 25565 jump players",
				"meta l4proto { tcp, udp } th dport 19132 jump players",
				"elements = { 10.0.0.0/8, 100.64.0.0/10, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16 }",
				"ip saddr != @allow4 counter name in_refused drop",
				"ip6 saddr != @allow6 counter name in_refused drop",
				"update @flood4 { ip saddr limit rate over 20/minute burst 20 packets } counter name in_flood drop",
				"tcp dport 25575 counter name in_closed drop",
				"udp dport 25565 counter name in_closed drop",
				"elements = { 127.0.0.53, 192.168.1.1 }",
				"elements = { fd00::1 }",
				"oifname \"lo\" counter name out_local goto refuse",
				"ip daddr @lan4 counter name out_lan goto refuse",
				"tcp dport { 80, 443 } accept",
				"udp dport { 3478, 19302 } accept",
				"counter name out_web goto refuse",
				"meta pkttype broadcast udp dport 7551 ip saddr @lan4 ip saddr @allow4 update @seek4 { ip saddr . udp sport }",
				"udp sport 7551 ip daddr . udp dport @seek4 accept",
			}, nil},
		{"any, open", []string{"any"}, "open", true,
			[]string{"th dport 19132 jump players", "ip daddr @lan4 counter name out_lan goto refuse", "udp dport 7551 ip saddr @lan4 update @seek4"},
			[]string{"@allow4", "set allow4", "dport { 80, 443 }", "3478", "out_web goto"}},
		{"no bedrock", []string{"lan"}, "web", false,
			[]string{"tcp dport { 80, 443 } accept"},
			[]string{"19132", "3478", "7551", "seek4"}},
		{"addresses", []string{"lan", "203.0.113.7", "198.51.100.0/24", "2001:db8::/32", "10.1.2.3"}, "web", true,
			[]string{
				"192.168.0.0/16, 203.0.113.7, 198.51.100.0/24, 10.1.2.3 }",
				"elements = { fc00::/7, fe80::/10, 2001:db8::/32 }",
			}, nil},
		{"v6 only", []string{"2001:db8::7"}, "open", false,
			[]string{"ip saddr != @allow4", "elements = { 2001:db8::7 }"},
			[]string{"3478"}},
	}
	checked := false
	for _, tc := range cases {
		c := config.Defaults()
		c.AllowFrom, c.Egress, c.Bedrock = tc.allow, tc.egress, tc.bedrock
		rules := Rules(&c, Local{DNS: dns})
		if !strings.HasPrefix(rules, "#!/usr/sbin/nft -f\n") || !strings.Contains(rules, "table inet tt_minecraftd\ndelete table inet tt_minecraftd\n") {
			t.Errorf("%s: no atomic swap at the top:\n%s", tc.name, rules)
		}
		if !strings.Contains(rules, "\t\tmeta skuid \"minecraftd\" jump server\n\t}") || strings.Contains(rules, "skuid !=") {
			t.Errorf("%s: output chain isn't the account's", tc.name)
		}
		for _, s := range tc.has {
			if !strings.Contains(rules, s) {
				t.Errorf("%s: no %q in\n%s", tc.name, s, rules)
			}
		}
		for _, s := range tc.hasNot {
			if strings.Contains(rules, s) {
				t.Errorf("%s: has %q", tc.name, s)
			}
		}
		// nft looks the account up, so check with one that's here.
		c.RunUser = me.Username
		checked = nftCheck(t, Rules(&c, Local{DNS: dns}))
	}
	if !checked {
		t.Log("nft -c not run: no unprivileged network namespace")
	}

	// A DNS rule comes before the loopback and LAN drops, or a local
	// resolver couldn't be reached.
	c := config.Defaults()
	rules := Rules(&c, Local{DNS: dns})
	if strings.Index(rules, "th dport 53 ip daddr @dns4 accept") > strings.Index(rules, `oifname "lo"`) {
		t.Error("DNS comes after the loopback drop")
	}
	// The answer to a LAN discovery broadcast comes before the LAN drop.
	if strings.Index(rules, "udp sport 7551") > strings.Index(rules, "out_lan goto refuse") {
		t.Error("discovery answer comes after the LAN drop")
	}

	c.GamePort, c.BedrockPort = 25575, 25576
	if rules := Rules(&c, Local{}); strings.Contains(rules, "tcp dport 25575 counter name in_closed") {
		t.Error("gamePort on the RCON port got dropped")
	}
	c.JoinRateLimit = 5
	if rules := Rules(&c, Local{}); !strings.Contains(rules, "limit rate over 5/minute burst 5 packets") {
		t.Error("joinRateLimit not used")
	}
}

// The server's own ports stay open to it on loopback, ahead of the drop,
// and nothing else on loopback does.
func TestOwnPortsOnLoopback(t *testing.T) {
	defer testid.Line(t, "df67w")
	c := config.Defaults()
	c.GamePort, c.BedrockPort = 25570, 19140
	rules := Rules(&c, Local{})
	drop := strings.Index(rules, `oifname "lo" counter name out_local goto refuse`)
	for _, s := range []string{`oifname "lo" tcp dport 25570 accept`, `oifname "lo" meta l4proto { tcp, udp } th dport 19140 accept`} {
		if i := strings.Index(rules, s); i < 0 || i > drop {
			t.Errorf("%q at %d, loopback drop at %d:\n%s", s, i, drop, rules)
		}
	}
	if n := strings.Count(rules, `oifname "lo"`); n != 3 {
		t.Errorf("%d loopback rules, want 3", n)
	}
	c.Bedrock = false
	if rules := Rules(&c, Local{}); strings.Contains(rules, "19140") {
		t.Error("bedrockPort open on loopback with bedrock off")
	}
}

func TestResolvers(t *testing.T) {
	defer testid.Line(t, "df66d")
	conf := "# by NetworkManager\nsearch lan\nnameserver 127.0.0.53\nnameserver  fe80::1%eth0\nnameserver bogus\nnameserver ::ffff:192.168.1.1\nnameserver 127.0.0.53\noptions edns0\n"
	got, err := Resolvers(strings.NewReader(conf))
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Addr{netip.MustParseAddr("127.0.0.53"), netip.MustParseAddr("fe80::1"), netip.MustParseAddr("192.168.1.1")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoaded(t *testing.T) {
	defer testid.Line(t, "df66e")
	var reply string
	var fail error
	r := &host.Fake{Reply: func(host.Cmd) ([]byte, error) { return []byte(reply), fail }}
	for _, tc := range []struct {
		reply string
		want  bool
	}{
		{"table inet filter\ntable inet tt_minecraftd\n", true},
		{"table inet tt_minecraftd_old\ntable inet filter\n", false},
		{"", false},
	} {
		reply = tc.reply
		got, err := Loaded(context.Background(), r)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %v, %v", tc.reply, got, err)
		}
	}
	if want := []string{"sudo nft list tables inet"}; !reflect.DeepEqual(r.Lines()[:1], want) || !r.Ran[0].Look {
		t.Errorf("ran %q", r.Lines())
	}
	fail = errors.New("sudo: a password is required")
	if _, err := Loaded(context.Background(), r); err == nil {
		t.Error("a failed list counts as an answer")
	}

	u := Unit()
	for _, s := range []string{"ExecStart=/usr/sbin/nft -f /etc/tt-minecraftd/firewall.nft", "ReloadPropagatedFrom=nftables.service", "PartOf=nftables.service", "After=nftables.service", "Before=network-pre.target", "WantedBy=sysinit.target nftables.service"} {
		if !strings.Contains(u, s) {
			t.Errorf("unit has no %q", s)
		}
	}
}

func TestCounters(t *testing.T) {
	defer testid.Line(t, "df67h")
	reply := `{"nftables": [{"metainfo": {"version": "1.1.6"}}, {"counter": {"family": "inet", "name": "out_lan", "table": "tt_minecraftd", "handle": 3, "packets": 12, "bytes": 720}}, {"counter": {"family": "inet", "name": "out_lan", "table": "other", "handle": 3, "packets": 5, "bytes": 0}}]}`
	var fail error
	r := &host.Fake{Reply: func(host.Cmd) ([]byte, error) { return []byte(reply), fail }}
	got, err := Counters(context.Background(), r)
	if err != nil || !reflect.DeepEqual(got, map[string]uint64{"out_lan": 12}) {
		t.Errorf("got %v, %v", got, err)
	}
	if want := []string{"nft -j list counters inet"}; !reflect.DeepEqual(r.Lines(), want) || !r.Ran[0].Look {
		t.Errorf("ran %q", r.Lines())
	}
	fail = errors.New("exit status 1")
	if _, err := Counters(context.Background(), r); err == nil {
		t.Error("a failed list counts as an answer")
	}
	fail = nil

	// nft's own answer, with the rules loaded and with none.
	if !nftCheck(t, "") {
		return
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.RunUser = me.Username
	path := filepath.Join(t.TempDir(), "firewall.nft")
	if err := os.WriteFile(path, []byte(Rules(&cfg, Local{})), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, load := range []bool{true, false} {
		script := "/usr/sbin/nft -j list counters inet"
		if load {
			script = "/usr/sbin/nft -f " + path + " && " + script
		}
		// #nosec G204 -- fixed binary, test file
		out, err := exec.Command("unshare", "-rn", "sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		reply = string(out)
		got, err := Counters(context.Background(), r)
		want := map[string]uint64{}
		if load {
			want = map[string]uint64{"in_refused": 0, "in_flood": 0, "in_closed": 0, "out_local": 0, "out_lan": 0, "out_web": 0}
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %v: got %v, %v, from %s", load, got, err, out)
		}
	}
}

// route is one /proc/net/ipv6_route line.
func route(dest string, bits int, gateway string, flags uint32, dev string) string {
	hex16 := func(s string) string {
		a := netip.MustParseAddr(s).As16()
		return fmt.Sprintf("%x", a[:])
	}
	return fmt.Sprintf("%s %02x %s 00 %s 00000100 00000001 00000000 %08x %8s\n", hex16(dest), bits, hex16("::"), hex16(gateway), flags, dev)
}

// The host's own subnets are the on-link routes, and nothing a VPN or the
// kernel's local table adds.
func TestOnLink(t *testing.T) {
	defer testid.Line(t, "df7a1")
	list := route("2001:db8:1:2::", 64, "::", 0x1, "eth0") + // RA's on-link prefix
		route("::", 0, "fe80::1", 0x450003, "eth0") + // default through the router
		route("2001:db8:5::", 48, "fe80::1", 0x3, "eth0") + // routed elsewhere
		route("2001:db8:1:2::abcd", 128, "::", 0x80200001, "eth0") + // own address, local table
		route("2001:db8:1:2::", 128, "::", 0x100001, "eth0") + // subnet anycast
		route("2001:db8:9::7", 128, "::", 0x1000001, "eth0") + // cached clone
		route("::", 0, "::", 0x1, "wg0") + // a VPN's whole internet
		route("2000::", 3, "::", 0x1, "tun0") + // or a big part of it
		route("2001:db8:aa::", 64, "::", 0x201, "eth1") + // unreachable
		route("2001:db8:bb::", 64, "::", 0x1, "lo") +
		route("fe80::", 64, "::", 0x1, "eth0") +
		route("fd00:1::", 64, "::", 0x1, "eth0") +
		route("ff00::", 8, "::", 0x1, "eth0") +
		route("2001:db8:7:100::", 56, "::", 0x1, "br0") + // a container bridge
		route("2001:db8:7:120::", 64, "::", 0x1, "br0") + // inside the /56
		route("2001:db8:1:2::", 64, "::", 0x1, "eth0") + // twice, other table
		"garbage line\n"
	got, err := OnLink(strings.NewReader(list))
	want := []netip.Prefix{netip.MustParsePrefix("2001:db8:1:2::/64"), netip.MustParsePrefix("2001:db8:7:100::/56")}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, %v, want %v", got, err, want)
	}
	// This box's own list reads without an error.
	if f, err := os.Open("/proc/net/ipv6_route"); err == nil {
		defer f.Close()
		if _, err := OnLink(f); err != nil {
			t.Error(err)
		}
	}
}

// The server can't reach the host's IPv6 subnets, multicast, or broadcast,
// whatever egress allows, and the LAN discovery answer still goes ahead.
func TestLANDropCoversSubnetsAndMulticast(t *testing.T) {
	defer testid.Line(t, "df7a2")
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	nets := []netip.Prefix{netip.MustParsePrefix("2001:db8:1:2::/64"), netip.MustParsePrefix("2001:db8:1:2::/64"), netip.MustParsePrefix("fd00::/64")}
	for _, egress := range []string{"web", "open"} {
		c := config.Defaults()
		c.Egress, c.Bedrock = egress, true
		rules := Rules(&c, Local{Nets: nets})
		lan6 := "\tset lan6 {\n\t\ttype ipv6_addr\n\t\tflags interval\n\t\tauto-merge\n\t\telements = { fc00::/7, fe80::/10, 2001:db8:1:2::/64 }\n\t}"
		if !strings.Contains(rules, lan6) {
			t.Errorf("%s: no %q in\n%s", egress, lan6, rules)
		}
		// allowFrom's lan stays the private ranges.
		if !strings.Contains(rules, "\tset allow6 {\n\t\ttype ipv6_addr\n\t\tflags interval\n\t\tauto-merge\n\t\telements = { fc00::/7, fe80::/10 }\n") {
			t.Errorf("%s: allow6 changed:\n%s", egress, rules)
		}
		answer := strings.Index(rules, "udp sport 7551 ip daddr . udp dport @seek4 accept")
		last := strings.Index(rules, "\tchain refuse {")
		if egress == "web" {
			last = strings.Index(rules, "tcp dport { 80, 443 } accept")
		}
		for _, s := range []string{"ip6 daddr @lan6 counter name out_lan goto refuse", "ip daddr { 224.0.0.0/4, 255.255.255.255 } counter name out_lan goto refuse", "ip6 daddr ff00::/8 counter name out_lan goto refuse"} {
			if i := strings.Index(rules, s); i < answer || i > last {
				t.Errorf("%s: %q at %d, want between %d and %d", egress, s, i, answer, last)
			}
		}
		c.RunUser = me.Username
		nftCheck(t, Rules(&c, Local{Nets: nets}))
	}
}

// start's view of the rules file: the config and the host's resolvers and
// subnets have to give it, apart from a subnet the host has lost.
func TestMatches(t *testing.T) {
	defer testid.Line(t, "df7a3")
	dir := t.TempDir()
	f := Files{Rules: filepath.Join(dir, "firewall.nft"), ResolvConf: filepath.Join(dir, "resolv.conf"), Routes: filepath.Join(dir, "ipv6_route")}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := config.Defaults()
	home, other := route("2001:db8:1:2::", 64, "::", 0x1, "eth0"), route("2001:db8:3:4::", 64, "::", 0x1, "eth0")
	write(f.ResolvConf, "nameserver 192.168.1.1\n")
	write(f.Routes, home)
	l, err := ReadLocal(f)
	if err != nil || len(l.Nets) != 1 || len(l.DNS) != 1 {
		t.Fatalf("read %v, %v", l, err)
	}
	if ok, err := Matches(&c, f); ok || err != nil {
		t.Errorf("no file: %v, %v", ok, err)
	}
	written := Rules(&c, l)
	write(f.Rules, written)
	check := func(what string, want bool) {
		t.Helper()
		if ok, err := Matches(&c, f); ok != want || err != nil {
			t.Errorf("%s: got %v, %v, want %v", what, ok, err, want)
		}
	}
	check("as written", true)
	for _, change := range []func(*config.Config){
		func(c *config.Config) { c.AllowFrom = []string{"any"} },
		func(c *config.Config) { c.Egress = "open" },
		func(c *config.Config) { c.JoinRateLimit = 5 },
		func(c *config.Config) { c.RunUser = "mc" },
		func(c *config.Config) { c.Bedrock = !c.Bedrock },
	} {
		was := c
		change(&c)
		check(fmt.Sprintf("config %+v", c), false)
		c = was
	}
	write(f.ResolvConf, "nameserver 192.168.1.2\n")
	check("new resolver", false)
	write(f.ResolvConf, "nameserver 192.168.1.1\n")
	write(f.Routes, home+other)
	check("new subnet", false)
	write(f.Routes, "")
	check("subnet gone, or not up yet", true)
	if err := os.Remove(f.Routes); err != nil {
		t.Fatal(err)
	}
	check("IPv6 off", true)
	write(f.Rules, strings.Replace(written, "drop\n", "accept\n", 1))
	check("edited by hand", false)
	write(f.Rules, strings.Replace(written, "2001:db8:1:2::/64", "2001:db8:1::/48", 1))
	write(f.Routes, home)
	check("a wider subnet", true)
}

// The rules file names the config it was written for, so a second config's
// repair can leave it alone, and start's check skips that line, since 2
// configs can give the same rules.
func TestOwnerLine(t *testing.T) {
	defer testid.Line(t, "df7a7")
	dir := t.TempDir()
	f := Files{Rules: filepath.Join(dir, "firewall.nft"), ResolvConf: filepath.Join(dir, "resolv.conf"), Routes: filepath.Join(dir, "ipv6_route")}
	if err := os.WriteFile(f.ResolvConf, []byte("nameserver 192.168.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := config.Defaults()
	l, err := ReadLocal(f)
	if err != nil {
		t.Fatal(err)
	}
	rules := Rules(&c, l)
	if Owner(rules) != "" || StripOwner(rules) != rules {
		t.Errorf("rules without the line: owner %q", Owner(rules))
	}
	for _, path := range []string{"/etc/tt-minecraftd/config.shcl", "/srv/a b/\"x\"\n# Config: \"/etc/y\".shcl", "/srv/mé/c.shcl"} {
		w := ForConfig(rules, path)
		if Owner(w) != path || StripOwner(w) != rules || !strings.HasPrefix(w, "#!/usr/sbin/nft -f\n# Written by") {
			t.Errorf("for %q: owner %q, wrote\n%s", path, Owner(w), w)
		}
		if err := os.WriteFile(f.Rules, []byte(w), 0o600); err != nil {
			t.Fatal(err)
		}
		if ok, err := Matches(&c, f); !ok || err != nil {
			t.Errorf("for %q: Matches %v, %v", path, ok, err)
		}
		nftCheck(t, w)
	}
	if Owner(ForConfig(rules, "")) != config.DefaultPath {
		t.Error("an empty path isn't the default config")
	}
	// Only the header counts.
	if Owner(rules+"# Config: \"/etc/z.shcl\"\n") != "" {
		t.Error("read a line past the header")
	}
}
