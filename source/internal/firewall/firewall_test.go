// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package firewall

import (
	"context"
	"errors"
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
			}, nil},
		{"any, open", []string{"any"}, "open", true,
			[]string{"th dport 19132 jump players", "ip daddr @lan4 counter name out_lan goto refuse"},
			[]string{"@allow4", "set allow4", "dport { 80, 443 }", "3478", "out_web goto"}},
		{"no bedrock", []string{"lan"}, "web", false,
			[]string{"tcp dport { 80, 443 } accept"},
			[]string{"19132", "3478"}},
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
		rules := Rules(&c, dns)
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
		checked = nftCheck(t, Rules(&c, dns))
	}
	if !checked {
		t.Log("nft -c not run: no unprivileged network namespace")
	}

	// A DNS rule comes before the loopback and LAN drops, or a local
	// resolver couldn't be reached.
	c := config.Defaults()
	rules := Rules(&c, dns)
	if strings.Index(rules, "th dport 53 ip daddr @dns4 accept") > strings.Index(rules, `oifname "lo"`) {
		t.Error("DNS comes after the loopback drop")
	}

	c.GamePort, c.BedrockPort = 25575, 25576
	if rules := Rules(&c, nil); strings.Contains(rules, "tcp dport 25575 counter name in_closed") {
		t.Error("gamePort on the RCON port got dropped")
	}
	c.JoinRateLimit = 5
	if rules := Rules(&c, nil); !strings.Contains(rules, "limit rate over 5/minute burst 5 packets") {
		t.Error("joinRateLimit not used")
	}
}

// The server's own ports stay open to it on loopback, ahead of the drop,
// and nothing else on loopback does.
func TestOwnPortsOnLoopback(t *testing.T) {
	defer testid.Line(t, "df67w")
	c := config.Defaults()
	c.GamePort, c.BedrockPort = 25570, 19140
	rules := Rules(&c, nil)
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
	if rules := Rules(&c, nil); strings.Contains(rules, "19140") {
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
	if err := os.WriteFile(path, []byte(Rules(&cfg, nil)), 0o600); err != nil {
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
