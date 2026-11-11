// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package config

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func mustLoad(t *testing.T, path string, opts ...Option) (Config, []Warning) {
	t.Helper()
	c, warns, err := Load(path, opts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c, warns
}

func asOperator(t *testing.T, sudoUser, user string) {
	t.Helper()
	t.Setenv("SUDO_USER", sudoUser)
	t.Setenv("USER", user)
}

// Each default in design.md's table that's written as a value, such as `4G`
// or `[lan]`, is what Load gives with no file. Rows in words are skipped.
func TestDefaultsMatchDesign(t *testing.T) {
	defer testid.Line(t, "df5x6")
	asOperator(t, "alice", "")
	text, err := os.ReadFile("../../../project/design.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(text), "### Configuration model")
	section, _, _ = strings.Cut(section, "\n### ")
	rows := regexp.MustCompile("(?m)^\\| `([A-Za-z]+)` +\\| `([^`<]+)` *(\\||$)").FindAllStringSubmatch(section, -1)
	if len(rows) < 15 {
		t.Fatalf("found only %d defaults in design.md", len(rows))
	}
	got, warns := mustLoad(t, "")
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	for _, row := range rows {
		want := got
		if err := want.Set(row[1], row[2]); err != nil {
			t.Errorf("design.md default for %s: %v", row[1], err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: design.md says %s, got %+v", row[1], row[2], got)
		}
	}
}

func TestLoadLayersFileThenOptions(t *testing.T) {
	defer testid.Line(t, "df5x7")
	asOperator(t, "alice", "")
	path := writeConfig(t, "heapMax: 8G\ngamePort: 25570\nrunUser: mcd\n")
	c, warns := mustLoad(t, path, Option{"gamePort", "25580"}, Option{"RUNMODE", "Native"}, Option{"allowFrom", "[lan, 203.0.113.7]"})
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := Defaults()
	want.HeapMax = 8 * gib
	want.HeapMin = gib
	want.MemCap = 11 * gib
	want.GamePort = 25580
	want.RunMode = "native"
	want.RunUser = "mcd"
	want.RunGroup = "mcd"
	want.OwnerAfterStop = "alice"
	want.OwnerTrees = []string{want.ServerDir}
	want.AllowFrom = []string{"lan", "203.0.113.7"}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got  %+v\nwant %+v", c, want)
	}

	// The last of 2 options wins, and a derived key follows the option.
	c, _ = mustLoad(t, path, Option{"runUser", "one"}, Option{"runUser", "two"}, Option{"serverDir", "/srv/x"})
	if c.RunUser != "two" || c.RunGroup != "two" || !reflect.DeepEqual(c.OwnerTrees, []string{"/srv/x"}) {
		t.Errorf("got user %s group %s trees %v", c.RunUser, c.RunGroup, c.OwnerTrees)
	}
}

func TestDerivedDefaults(t *testing.T) {
	defer testid.Line(t, "df5x8")
	cases := []struct {
		sudoUser, user, file string
		check                func(c Config) bool
	}{
		{"alice", "root", "", func(c Config) bool { return c.OwnerAfterStop == "alice" }},
		{"", "bob", "", func(c Config) bool { return c.OwnerAfterStop == "bob" }},
		{"-x", "bob", "", func(c Config) bool { return c.OwnerAfterStop == "bob" }},
		{"", "", "", func(c Config) bool { return c.OwnerAfterStop == "" }},
		{"alice", "", "ownerAfterStop: carol\n", func(c Config) bool { return c.OwnerAfterStop == "carol" }},
		{"", "", "runUser: mcd\nrunGroup: games\n", func(c Config) bool { return c.RunGroup == "games" }},
		{"", "", "serverDir: /srv/mc\nownerTrees: [/srv/mc, /tank/w]\n", func(c Config) bool { return len(c.OwnerTrees) == 2 }},
		{"", "", "heapMax: 512M\n", func(c Config) bool { return c.HeapMin == 512<<20 && c.MemCap == 512<<20+128<<20+gib }},
		{"", "", "heapMin: 2G\nheapMax: 6G\nmemCap: 7G\n", func(c Config) bool { return c.HeapMin == 2*gib && c.MemCap == 7*gib }},
	}
	for _, tc := range cases {
		asOperator(t, tc.sudoUser, tc.user)
		path := ""
		if tc.file != "" {
			path = writeConfig(t, tc.file)
		}
		c, warns := mustLoad(t, path)
		if len(warns) != 0 || !tc.check(c) {
			t.Errorf("%q %q %q: got %+v, warnings %v", tc.sudoUser, tc.user, tc.file, c, warns)
		}
	}
}

func TestClashingKeysWarn(t *testing.T) {
	defer testid.Line(t, "df5x9")
	asOperator(t, "alice", "")
	c, warns := mustLoad(t, writeConfig(t, "heapMin: 6G\nmemCap: 3G\n"))
	got := make([]string, len(warns))
	for i, w := range warns {
		got[i] = w.String()
	}
	want := []string{
		"heapMin 6G is over heapMax 4G; using 4G for both",
		"memCap 3G isn't over heapMax 4G; using the default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if c.HeapMin != 4*gib || c.HeapMax != 4*gib || c.MemCap != 6*gib {
		t.Errorf("got heap %d to %d, cap %d", c.HeapMin, c.HeapMax, c.MemCap)
	}
}

func TestBadOptionIsAnError(t *testing.T) {
	defer testid.Line(t, "df5xa")
	cases := []struct{ key, value, says string }{
		{"whiteList", "true", `unknown key "whiteList"`},
		{"heapMax.min", "1G", "unknown key"},
		{"runMode", "podman", `runMode: "podman" isn't one of`},
		{"runMode", "native\nfirewall: off", "takes one line"},
		{"runMode", "", "no value"},
		{"gamePort", "[1, 2]", "takes one value"},
		{"allowFrom", "[lan, everyone]", `"everyone" isn't lan`},
		{"allowFrom", "[lan", "allowFrom: "},
		{"serverDir", "/", "can't be /"},
	}
	for _, tc := range cases {
		_, _, err := Load("", []Option{{tc.key, tc.value}})
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s=%q: got %v", tc.key, tc.value, err)
		}
		c := Defaults()
		if err := c.Set(tc.key, tc.value); err == nil || !reflect.DeepEqual(c, Defaults()) {
			t.Errorf("%s=%q: Set gave %v and changed the config to %+v", tc.key, tc.value, err, c)
		}
	}
}

// A bad line costs only its own key. Options still apply on top.
func TestBadLinesDontSinkLoad(t *testing.T) {
	defer testid.Line(t, "df5xb")
	asOperator(t, "alice", "")
	path := writeConfig(t, "gamePort: 70000\nthis line has no colon\nheapMax: 6G\negress: lan\nfirewall: off\n")
	c, warns := mustLoad(t, path, Option{"egress", "open"})
	if len(warns) != 3 {
		t.Errorf("want 3 warnings, got %v", warns)
	}
	if c.GamePort != 25565 || c.HeapMax != 6*gib || c.Egress != "open" || c.Firewall {
		t.Errorf("got port %d heap %d egress %s firewall %v", c.GamePort, c.HeapMax, c.Egress, c.Firewall)
	}
	if _, _, err := Load(path+".gone", nil); err == nil {
		t.Error("a missing file loaded")
	}
}

// Geyser's signaling can't share gamePort's TCP port, so bedrockPort moves off it.
func TestBedrockPortClash(t *testing.T) {
	defer testid.Line(t, "df5yz")
	asOperator(t, "alice", "")
	for _, c := range []struct {
		text string
		port int
		warn string
	}{
		{"bedrockPort: 25565\n", 19132, "bedrockPort 25565 is gamePort too; using 19132"},
		{"gamePort: 19132\n", 19133, "bedrockPort 19132 is gamePort too; using 19133"},
		{"gamePort: 19132\nbedrock: false\n", 19132, ""},
		{"bedrockPort: 19140\n", 19140, ""},
	} {
		got, warns := mustLoad(t, writeConfig(t, c.text))
		var text string
		if len(warns) > 0 {
			text = warns[0].String()
		}
		if got.BedrockPort != c.port || text != c.warn || len(warns) > 1 {
			t.Errorf("%q: bedrockPort %d, warnings %v; want %d, %q", c.text, got.BedrockPort, warns, c.port, c.warn)
		}
	}
}
