// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/firewall"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
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
	defer testid.Line(t, "df66g")
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
	rules := firewall.Rules(h.Cfg, []netip.Addr{netip.MustParseAddr("192.168.1.1")})

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

	fakeNft(t, false)
	if got := look(t, h, FirewallCheck); !reflect.DeepEqual(got, []string{"nftables isn't installed, so the firewall can't load; apt-get install nftables adds it"}) {
		t.Errorf("no nft: %q", got)
	}

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
