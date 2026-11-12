// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.shcl")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRead(t *testing.T, text string, c *Config) []Warning {
	t.Helper()
	warns, err := Read(writeConfig(t, text), c)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return warns
}

// Stand-in defaults, so a test can tell "left alone" from "zeroed".
func before() Config {
	return Config{
		RunMode:      "docker",
		RunUser:      "minecraftd",
		ServerDir:    "/srv/tt-minecraftd/server",
		HeapMax:      4 << 30,
		GamePort:     25565,
		Bedrock:      true,
		AllowFrom:    []string{"lan"},
		Egress:       "web",
		Firewall:     true,
		StartTimeout: 300 * time.Second,
	}
}

func TestReadEveryKey(t *testing.T) {
	defer testid.Line(t, "df5wt")
	text := `runMode: native
runUser: mcd
runGroup: games
ownerAfterStop: alice
serverDir: /srv/mc/server/
ownerTrees: [/srv/mc/server, /tank/world]
zfsDataset: tank/minecraft
image: docker.io/library/eclipse-temurin@sha256:abc123
javaBin: /usr/lib/jvm/java-25/bin/java
heapMin: 2G
heapMax: 6 GiB
memCap: 8192M
nice: 5
jvmArgs: [-XX:+UseG1GC, -XX:MaxGCPauseMillis=200]
extraJvmArgs: -Dfoo=bar
bindIP: 192.168.1.123
gamePort: 25570
bedrock: no
bedrockPort: 19133
allowFrom: [lan, 203.0.113.7, 198.51.100.9/24]
joinRateLimit: 5
egress: open
hideOnlinePlayers: false
startTimeout: 10m
stopTimeout: 90
firewall: off
startAtBoot: yes
extraArgs: [--nogui]
agent: Command
agentURL: http://127.0.0.1:11434/v1/
agentModel: qwen3:32b
agentCommand: [/usr/local/bin/llm, -m, gpt-5]
agentHosts: [CDN.Modrinth.com, ci.example.org]
`
	var c Config
	if warns := mustRead(t, text, &c); len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := Config{
		RunMode:           "native",
		RunUser:           "mcd",
		RunGroup:          "games",
		OwnerAfterStop:    "alice",
		ServerDir:         "/srv/mc/server",
		OwnerTrees:        []string{"/srv/mc/server", "/tank/world"},
		ZfsDataset:        "tank/minecraft",
		Image:             "docker.io/library/eclipse-temurin@sha256:abc123",
		JavaBin:           "/usr/lib/jvm/java-25/bin/java",
		HeapMin:           2 << 30,
		HeapMax:           6 << 30,
		MemCap:            8 << 30,
		Nice:              5,
		JvmArgs:           []string{"-XX:+UseG1GC", "-XX:MaxGCPauseMillis=200"},
		ExtraJvmArgs:      []string{"-Dfoo=bar"},
		BindIP:            "192.168.1.123",
		GamePort:          25570,
		Bedrock:           false,
		BedrockPort:       19133,
		AllowFrom:         []string{"lan", "203.0.113.7", "198.51.100.0/24"},
		JoinRateLimit:     5,
		Egress:            "open",
		HideOnlinePlayers: false,
		StartTimeout:      10 * time.Minute,
		StopTimeout:       90 * time.Second,
		Firewall:          false,
		StartAtBoot:       true,
		ExtraArgs:         []string{"--nogui"},
		Agent:             "command",
		AgentURL:          "http://127.0.0.1:11434/v1",
		AgentModel:        "qwen3:32b",
		AgentCommand:      []string{"/usr/local/bin/llm", "-m", "gpt-5"},
		AgentHosts:        []string{"cdn.modrinth.com", "ci.example.org"},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got  %+v\nwant %+v", c, want)
	}
}

func TestMissingKeysLeaveDefaults(t *testing.T) {
	defer testid.Line(t, "df5wv")
	c := before()
	if warns := mustRead(t, "## nothing set\n\nheapMax: 8G\n", &c); len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := before()
	want.HeapMax = 8 << 30
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got  %+v\nwant %+v", c, want)
	}
}

func TestBadValueWarnsAndKeepsDefault(t *testing.T) {
	defer testid.Line(t, "df5ww")
	cases := []struct{ line, says string }{
		{"runMode: lxc", `"lxc" isn't one of: podman, docker, native`},
		{"runMode: [docker, native]", "takes one value"},
		{"runMode:", "no value"},
		{"runUser: -root", "isn't a user or group name"},
		{"runUser: two words", "isn't a user or group name"},
		{"serverDir: srv/mc", "isn't a full path"},
		{"serverDir: /", "can't be /"},
		{"ownerTrees: [/srv/mc, /]", "can't be /"},
		{"zfsDataset: /tank/mc", "isn't a ZFS dataset name"},
		{"zfsDataset: tank/mc@snap", "isn't a ZFS dataset name"},
		{"image: --privileged", "isn't an image name"},
		{"image: eclipse-temurin:25-jre", "has no registry"},
		{"image: library/eclipse-temurin", "has no registry"},
		{"javaBin: bin/java", "isn't a full path or a program name"},
		{"heapMax: 4X", `"4X" isn't a size`},
		{"heapMax: 0G", "too small"},
		{"heapMax: 99999999999T", "isn't a size"},
		{"nice: 20", "out of range, -20 to 19"},
		{"nice: high", "isn't a whole number"},
		{"jvmArgs: [-Xss1M, \"\"]", "empty item"},
		{"bindIP: 300.1.1.1", "isn't an IP address"},
		{"bindIP: fe80::1%eth0", "isn't an IP address"},
		{"gamePort: 0", "out of range, 1 to 65535"},
		{"gamePort: 70000", "out of range"},
		{"bedrock: maybe", `"maybe" isn't true or false`},
		{"allowFrom: [lan, everyone]", `"everyone" isn't lan, any`},
		{"allowFrom: 10.0.0.0/33", "isn't an address range"},
		{"joinRateLimit: 0", "out of range"},
		{"egress: lan", "isn't one of: web, open"},
		{"startTimeout: -5", "isn't a number of seconds"},
		{"startTimeout: 0", "too short"},
		{"stopTimeout: soon", "isn't a number of seconds"},
		{"agent: chatgpt", "isn't one of: none, openai, anthropic, command"},
		{"agentURL: http://192.168.1.5:11434/v1", "plain http only goes to a loopback address"},
		{"agentURL: http://localhost.example.com/v1", "plain http only goes to a loopback address"},
		{"agentURL: https://me:sk-123@api.example.com", "can't hold a user or key"},
		{"agentURL: ftp://api.example.com", "isn't https"},
		{"agentURL: api.openai.com/v1", "isn't a web address"},
		{"agentModel: \"gpt 5\"", "isn't a model name"},
		{"agentModel: --help", "isn't a model name"},
		{"agentCommand: []", "takes a program and its arguments"},
		{"agentCommand: [bin/llm, -m]", "isn't a full path or a program name"},
		{"agentHosts: [cdn.modrinth.com, https://github.com]", "isn't a host name"},
		{"agentHosts: localhost", "isn't a host name"},
	}
	for _, tc := range cases {
		c := before()
		warns := mustRead(t, "## bad\n"+tc.line+"\n", &c)
		if len(warns) != 1 {
			t.Errorf("%s: want 1 warning, got %v", tc.line, warns)
			continue
		}
		w := warns[0]
		key, _, _ := strings.Cut(tc.line, ":")
		if w.Line != 2 || !strings.Contains(w.Text, tc.says) || !strings.HasPrefix(w.Text, key+": ") ||
			!strings.HasSuffix(w.Text, "using the default") {
			t.Errorf("%s: got %q at line %d", tc.line, w.Text, w.Line)
		}
		if !reflect.DeepEqual(c, before()) {
			t.Errorf("%s: changed the config to %+v", tc.line, c)
		}
	}
}

func TestUnknownNestedAndRepeatedKeysWarn(t *testing.T) {
	defer testid.Line(t, "df5wx")
	c := before()
	warns := mustRead(t, "gamePort: 25570\nwhiteList: true\nheapMax: 8G\n\tmin: 1G\ngamePort: 25571\negress: open\n", &c)
	got := make([]string, len(warns))
	for i, w := range warns {
		got[i] = strings.TrimPrefix(w.String(), filepath.Dir(w.File)+"/")
	}
	want := []string{
		`config.shcl:4: unknown key "min" under heapMax`,
		`config.shcl:1: gamePort is set on lines 1, 5; using the default`,
		`config.shcl:2: unknown key "whiteList"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if c.GamePort != 25565 || c.HeapMax != 8<<30 || c.Egress != "open" {
		t.Errorf("got port %d heap %d egress %s", c.GamePort, c.HeapMax, c.Egress)
	}
}

func TestSizesTakeJavaAndShclForms(t *testing.T) {
	defer testid.Line(t, "df5wy")
	cases := map[string]int64{
		"512m":    512 << 20,
		"512M":    512 << 20,
		"4g":      4 << 30,
		"1T":      1 << 40,
		"2048k":   2 << 20,
		"4GB":     4 << 30,
		"4 GiB":   4 << 30,
		"1.5GB":   3 << 29,
		"512 MiB": 512 << 20,
	}
	for spelled, want := range cases {
		var c Config
		if warns := mustRead(t, "heapMax: "+spelled+"\n", &c); len(warns) != 0 {
			t.Errorf("%s: %v", spelled, warns)
		}
		if c.HeapMax != want {
			t.Errorf("%s: got %d, want %d", spelled, c.HeapMax, want)
		}
	}
}

func TestKeysAndChoicesIgnoreCase(t *testing.T) {
	defer testid.Line(t, "df5wz")
	var c Config
	if warns := mustRead(t, "RunMode: Docker\nALLOWFROM: [LAN, Any]\nEgress: WEB\n", &c); len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if c.RunMode != "docker" || c.Egress != "web" || !reflect.DeepEqual(c.AllowFrom, []string{"lan", "any"}) {
		t.Errorf("got %+v", c)
	}
}

func TestBrokenLineWarnsAndRestLoads(t *testing.T) {
	defer testid.Line(t, "df5x0")
	c := before()
	warns := mustRead(t, "gamePort: 25570\nthis line has no colon\negress: open\n", &c)
	if len(warns) == 0 || warns[0].Line != 2 {
		t.Errorf("want a warning on line 2, got %v", warns)
	}
	if c.GamePort != 25570 || c.Egress != "open" {
		t.Errorf("got port %d egress %s", c.GamePort, c.Egress)
	}
}

func TestUnreadableFileIsAnError(t *testing.T) {
	defer testid.Line(t, "df5x1")
	c := before()
	_, err := Read(filepath.Join(t.TempDir(), "nope.shcl"), &c)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: got %v", err)
	}
	if _, err := Read(t.TempDir(), &c); err == nil {
		t.Error("a folder read as a config")
	}
	if !reflect.DeepEqual(c, before()) {
		t.Errorf("changed the config to %+v", c)
	}
}

// Every key in design.md's settings table is read, and nothing else.
func TestKeysMatchDesign(t *testing.T) {
	defer testid.Line(t, "df5x2")
	text, err := os.ReadFile("../../../project/design.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(text), "### Configuration model")
	section, _, _ = strings.Cut(section, "\n### ")
	rows := regexp.MustCompile("(?m)^\\| `([A-Za-z]+)` ").FindAllStringSubmatch(section, -1)
	if len(rows) < 20 {
		t.Fatalf("found only %d keys in design.md", len(rows))
	}
	var c Config
	setters := c.setters()
	for _, row := range rows {
		folded := strings.ToLower(row[1])
		if _, ok := setters[folded]; !ok {
			t.Errorf("design.md key %s isn't read", row[1])
		}
		delete(setters, folded)
	}
	for name := range setters {
		t.Errorf("%s is read but not in design.md", name)
	}
}
