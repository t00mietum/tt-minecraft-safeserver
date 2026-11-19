// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/firewall"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// fakeNft gives HasNft an nft of its own, or none.
func fakeNft(t *testing.T, there bool) {
	t.Helper()
	was := nftPaths
	nftPaths = []string{filepath.Join(t.TempDir(), "nft")}
	if there {
		if err := os.WriteFile(nftPaths[0], nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { nftPaths = was })
}

func TestFirewallCheck(t *testing.T) {
	defer testid.Line(t, "df66g00")
	fakeNft(t, true)
	var enabled, tables string
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) {
		switch c.Args[0] {
		case "systemctl":
			return []byte(enabled), nil
		case "nft":
			return []byte(tables), nil
		}
		return nil, nil
	})
	h.Cfg.Firewall, h.Cfg.AllowFrom, h.Cfg.Egress, h.Cfg.GamePort, h.Cfg.JoinRateLimit = true, []string{"lan"}, "web", 25565, 20
	if err := os.WriteFile(h.ResolvConf, []byte("nameserver 192.168.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := firewall.ForConfig(firewall.Rules(h.Cfg, firewall.Local{DNS: []netip.Addr{netip.MustParseAddr("192.168.1.1")}}), h.ConfigPath)

	// A new host: all 4 found, and fixed in order.
	enabled = "disabled\n"
	hasAll(t, "new", look(t, h, FirewallCheck), h.RulesFile+" is missing, or doesn't match the config [fix]",
		h.UnitFile+" is missing, or out of date [fix]", "isn't enabled, so the rules don't load at boot [fix]", "the loaded rules are out of date [fix]")
	want := []string{
		"sudo install -D -m 0644 -o root -g root -T /dev/stdin " + h.RulesFile,
		"sudo install -D -m 0644 -o root -g root -T /dev/stdin " + h.UnitFile,
		"sudo systemctl daemon-reload",
		"sudo systemctl enable tt-minecraftd-firewall.service",
		"sudo systemctl reload-or-restart tt-minecraftd-firewall.service",
	}
	if got := fixAll(t, h, f, FirewallCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("fixes ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if string(f.Ran[0].Stdin) != rules || string(f.Ran[1].Stdin) != firewall.Unit() {
		t.Errorf("wrote %q", f.Ran[0].Stdin)
	}

	// All in place.
	for path, text := range map[string]string{h.RulesFile: rules, h.UnitFile: firewall.Unit()} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	enabled, tables = "enabled\n", "table inet tt_minecraftd\n"
	if got := look(t, h, FirewallCheck); len(got) != 0 {
		t.Errorf("all in place: %q", got)
	}
	tables = ""
	if got := fixAll(t, h, f, FirewallCheck); !reflect.DeepEqual(got, want[4:]) {
		t.Errorf("table flushed: ran %q", got)
	}

	// A config change, or a new resolver, makes the file and the table stale.
	tables = "table inet tt_minecraftd\n"
	h.Cfg.AllowFrom = []string{"any"}
	if got := fixAll(t, h, f, FirewallCheck); !reflect.DeepEqual(got, []string{want[0], want[4]}) {
		t.Errorf("allowFrom changed: ran %q", got)
	}
	h.Cfg.AllowFrom = []string{"lan"}
	if err := os.WriteFile(h.ResolvConf, []byte("nameserver 192.168.1.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, FirewallCheck); len(got) != 2 {
		t.Errorf("resolver changed: %q", got)
	}
	// So does a new IPv6 subnet on the LAN, which goes in the file.
	if err := os.WriteFile(h.ResolvConf, []byte("nameserver 192.168.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, FirewallCheck); len(got) != 0 {
		t.Errorf("resolver back: %q", got)
	}
	route := "20010db8000100020000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001     eth0\n"
	if err := os.WriteFile(h.Routes, []byte(route), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fixAll(t, h, f, FirewallCheck); !reflect.DeepEqual(got, []string{want[0], want[4]}) || !strings.Contains(string(f.Ran[0].Stdin), "fe80::/10, 2001:db8:1:2::/64 }") {
		t.Errorf("subnet added: ran %q, wrote\n%s", got, f.Ran[0].Stdin)
	}
	if err := os.Remove(h.Routes); err != nil {
		t.Fatal(err)
	}

	fakeNft(t, false)
	if got := look(t, h, FirewallCheck); !reflect.DeepEqual(got, []string{"nftables isn't installed, so the firewall can't load; the distro's nftables package adds it"}) {
		t.Errorf("no nft, no apt: %q", got)
	}
	wasInstalled := launch.Installed
	launch.Installed = func(name string) bool { return name == "apt-get" }
	if got := look(t, h, FirewallCheck); !reflect.DeepEqual(got, []string{"nftables isn't installed, so the firewall can't load; apt-get install nftables adds it"}) {
		t.Errorf("no nft: %q", got)
	}
	launch.Installed = wasInstalled

	// Off: the unit goes, and so does a table left behind.
	fakeNft(t, true)
	h.Cfg.Firewall = false
	if got := fixAll(t, h, f, FirewallCheck); !reflect.DeepEqual(got, []string{
		"sudo systemctl disable --now tt-minecraftd-firewall.service",
		"sudo rm -f -- " + h.UnitFile,
		"sudo systemctl daemon-reload",
	}) {
		t.Errorf("off: ran %q", got)
	}
	if err := os.Remove(h.UnitFile); err != nil {
		t.Fatal(err)
	}
	if got := fixAll(t, h, f, FirewallCheck); !reflect.DeepEqual(got, []string{"sudo nft delete table inet tt_minecraftd"}) {
		t.Errorf("off, table left: ran %q", got)
	}
	tables = ""
	if got := look(t, h, FirewallCheck); len(got) != 0 {
		t.Errorf("off and gone: %q", got)
	}
	if _, err := os.Stat(h.RulesFile); err != nil {
		t.Errorf("off removed the rules file: %v", err)
	}
}

// There's one rules file and one table. A second config's repair leaves the
// first one's alone while its unit is there, and says whose they are. Rules
// that come out the same for both do for both.
func TestFirewallOtherConfig(t *testing.T) {
	defer testid.Line(t, "df7a900")
	fakeNft(t, true)
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) {
		switch c.Args[0] {
		case "systemctl":
			return []byte("enabled\n"), nil
		case "nft":
			return []byte("table inet tt_minecraftd\n"), nil
		}
		return nil, nil
	})
	h.Cfg.Firewall, h.Cfg.AllowFrom, h.Cfg.Egress, h.Cfg.GamePort, h.Cfg.JoinRateLimit = true, []string{"lan"}, "web", 25565, 20
	if err := os.WriteFile(h.ResolvConf, []byte("nameserver 192.168.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := h.ConfigPath
	local := firewall.Local{DNS: []netip.Addr{netip.MustParseAddr("192.168.1.1")}}
	firstRules := firewall.ForConfig(firewall.Rules(h.Cfg, local), first)
	for path, text := range map[string]string{h.RulesFile: firstRules, h.UnitFile: firewall.Unit()} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.ConfigPath = filepath.Join(filepath.Dir(first), "second.shcl")
	if got := look(t, h, FirewallCheck); len(got) != 0 {
		t.Errorf("same rules: %q", got)
	}

	h.Cfg.GamePort = 25566
	got := look(t, h, FirewallCheck)
	if len(got) != 1 || !strings.Contains(got[0], "is written for the config at "+first+", with other rules") || strings.HasSuffix(got[0], "[fix]") {
		t.Errorf("other rules: %q", got)
	}
	h.Cfg.Firewall = false
	var lines []string
	Run(context.Background(), h, []Check{FirewallCheck}, false, func(state, text string) { lines = append(lines, state+" "+text) })
	if want := []string{"OK tt-minecraftd firewall: " + h.RulesFile + " and tt-minecraftd-firewall.service are for the config at " + first + ", so they're left alone"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("off: %q", lines)
	}
	for _, on := range []bool{true, false} {
		h.Cfg.Firewall = on
		if ran := fixAll(t, h, f, FirewallCheck); len(ran) != 0 {
			t.Errorf("firewall %v changed the first config's: %q", on, ran)
		}
	}
	if have, err := os.ReadFile(h.RulesFile); err != nil || string(have) != firstRules {
		t.Errorf("rules file changed: %v", err)
	}

	// Once the first turns its firewall off, the file it left is anyone's.
	if err := os.Remove(h.UnitFile); err != nil {
		t.Fatal(err)
	}
	h.Cfg.Firewall = true
	if ran := fixAll(t, h, f, FirewallCheck); len(ran) != 4 || firewall.Owner(string(f.Ran[0].Stdin)) != h.ConfigPath {
		t.Errorf("left behind: ran %q", ran)
	}
	// So is one from an older build, which names no config.
	if err := os.WriteFile(h.UnitFile, []byte(firewall.Unit()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.RulesFile, []byte(firewall.StripOwner(firstRules)), 0o600); err != nil {
		t.Fatal(err)
	}
	if ran := fixAll(t, h, f, FirewallCheck); len(ran) != 2 {
		t.Errorf("older file: ran %q", ran)
	}
}
