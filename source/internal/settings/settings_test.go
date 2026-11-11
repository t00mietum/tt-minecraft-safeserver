// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func newServer(t *testing.T) (*config.Config, string) {
	t.Helper()
	cfg := config.Defaults()
	cfg.ServerDir = t.TempDir()
	return &cfg, cfg.ServerDir
}

func put(t *testing.T, dir, name, text string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, name), mode); err != nil {
		t.Fatal(err)
	}
}

func keys(problems []Problem) []string {
	var list []string
	for _, p := range problems {
		list = append(list, p.File+" "+p.Key)
	}
	return list
}

// A missing server.properties counts as unsafe, since Minecraft's own
// defaults have the whitelist off. Fix writes one.
func TestPropertiesMissing(t *testing.T) {
	defer testid.Line(t, "df5yk")
	cfg, dir := newServer(t)
	problems, err := Check(cfg)
	if err != nil || len(problems) != len(propertyRules(cfg)) || !problems[0].Missing {
		t.Fatalf("check: %v, %q", err, keys(problems))
	}
	if got := problems[1].String(); got != `server.properties: white-list isn't set, and should be "true"` {
		t.Errorf("String() = %q", got)
	}
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, propertiesFile))
	if err != nil || fi.Mode().Perm() != 0o660 {
		t.Errorf("new file: %v, mode %v; want 0660", err, fi.Mode())
	}
	if problems, err := Check(cfg); err != nil || len(problems) != 0 {
		t.Errorf("after fix: %v, %q", err, keys(problems))
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*")); len(left) != 0 {
		t.Errorf("temp files left: %q", left)
	}
}

// Fix changes only the wrong settings, and keeps comments, order, line ends,
// and the file's mode.
func TestPropertiesFix(t *testing.T) {
	defer testid.Line(t, "df5ym")
	cfg, dir := newServer(t)
	cfg.BindIP, cfg.GamePort, cfg.HideOnlinePlayers = "::1", 25570, false
	put(t, dir, propertiesFile, strings.Join([]string{
		"#Minecraft server properties",
		"motd=A Minecraft Server\\: hi",
		"online-mode=TRUE",
		"white-list=false",
		"enforce-whitelist = true",
		"enable-rcon=true",
		"enable-rcon=false",
		"enable-query:true\r",
		"management-server-enabled=false",
		"enable-jmx-monitoring=false",
		"server-ip=\\:\\:1",
		"server-port=25565",
		"hide-online-players=true",
		"",
	}, "\n"), 0o640)
	problems, err := Check(cfg)
	want := []string{
		"server.properties white-list",
		"server.properties enable-query",
		"server.properties server-port",
		"server.properties hide-online-players",
	}
	if err != nil || !reflect.DeepEqual(keys(problems), want) {
		t.Fatalf("check: %v, %q; want %q", err, keys(problems), want)
	}
	if got := problems[0].Fixed(); got != `server.properties: white-list is now "true", was "false"` {
		t.Errorf("Fixed() = %q", got)
	}
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, propertiesFile))
	if err != nil {
		t.Fatal(err)
	}
	wantText := strings.Join([]string{
		"#Minecraft server properties",
		"motd=A Minecraft Server\\: hi",
		"online-mode=TRUE",
		"white-list=true",
		"enforce-whitelist = true",
		"enable-rcon=true",
		"enable-rcon=false",
		"enable-query=false\r",
		"management-server-enabled=false",
		"enable-jmx-monitoring=false",
		"server-ip=\\:\\:1",
		"server-port=25570",
		"hide-online-players=false",
		"",
	}, "\n")
	if string(b) != wantText {
		t.Errorf("fixed file:\n%s\nwant:\n%s", b, wantText)
	}
	if fi, _ := os.Stat(filepath.Join(dir, propertiesFile)); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want 0640 kept", fi.Mode())
	}
}

func TestParseProperties(t *testing.T) {
	defer testid.Line(t, "df5yn")
	p := parseProperties([]byte("! bang comment\n  # hash comment\na = 1\nb:2\nc 3\nd=x\\\n    y\ne=\\u0041\\t\nf\ng=one\ng=two\n"))
	for key, want := range map[string]string{"a": "1", "b": "2", "c": "3", "d": "xy", "e": "A\t", "f": "", "g": "two"} {
		if got, ok := p.get(key); !ok || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, ok, want)
		}
	}
	if _, ok := p.get("#"); ok {
		t.Error("a comment read as a key")
	}
	if got := escape(" a=b:c#d!e\\f"); got != `\ a\=b\:c\#d\!e\\f` {
		t.Errorf("escape = %q", got)
	}
}

// The proxy settings in Paper's YAML files are found by their full path,
// and a fix keeps the comment and the rest of the file.
func TestYAMLSettings(t *testing.T) {
	defer testid.Line(t, "df5yp")
	cfg, dir := newServer(t)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	spigot := "config-version: 12\nadvancements:\n  bungeecord: true\nsettings:\n  debug: false\n  bungeecord: yes # proxy\n  timeout: 60\n"
	put(t, dir, "spigot.yml", spigot, 0o640)
	paper := "proxies:\n  bungee-cord:\n    online-mode: true\n  proxy-protocol: false\n  velocity:\n    enabled: \"true\"\n    secret: ''\nunsupported-settings:\n  enabled: true\n"
	put(t, dir, "config/paper-global.yml", paper, 0o640)
	problems, err := Check(cfg)
	want := []string{"spigot.yml settings.bungeecord", "config/paper-global.yml proxies.velocity.enabled"}
	if err != nil || !reflect.DeepEqual(keys(problems), want) {
		t.Fatalf("check: %v, %q; want %q", err, keys(problems), want)
	}
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "spigot.yml"))
	if got := string(b); got != strings.Replace(spigot, "bungeecord: yes # proxy", "bungeecord: false # proxy", 1) {
		t.Errorf("spigot.yml:\n%s", got)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "config/paper-global.yml"))
	if got := string(b); got != strings.Replace(paper, `enabled: "true"`, "enabled: false", 1) {
		t.Errorf("paper-global.yml:\n%s", got)
	}

	put(t, dir, "config/paper-global.yml", "proxies:\n  proxy-protocol: true\n", 0o640)
	if problems, _ := Check(cfg); !reflect.DeepEqual(keys(problems), []string{"config/paper-global.yml proxies.proxy-protocol"}) {
		t.Errorf("proxy-protocol: %q", keys(problems))
	}
	if i, ok := yamlFind([]string{"a:", "  b: 1", "c:", "  d: 2"}, []string{"a", "d"}); ok {
		t.Errorf("found a.d at %d, outside a", i)
	}
}

// The server account can write the tree, so links and pipes there are
// refused, and a fix never writes through one.
func TestSettingsLinks(t *testing.T) {
	defer testid.Line(t, "df5yq")
	cfg, dir := newServer(t)
	outside := t.TempDir()
	put(t, outside, propertiesFile, "white-list=false\n", 0o640)
	if err := os.Symlink(filepath.Join(outside, propertiesFile), filepath.Join(dir, propertiesFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := Fix(cfg); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("linked server.properties: %v", err)
	}

	cfg, dir = newServer(t)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	put(t, outside, "paper-global.yml", "proxies:\n  velocity:\n    enabled: true\n", 0o640)
	if err := os.Symlink(outside, filepath.Join(dir, "config")); err != nil {
		t.Fatal(err)
	}
	if _, err := Fix(cfg); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("linked config/: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "paper-global.yml")); !strings.Contains(string(b), "enabled: true") {
		t.Error("fix wrote through the link")
	}

	cfg, dir = newServer(t)
	if err := syscall.Mkfifo(filepath.Join(dir, propertiesFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(cfg); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Errorf("pipe: %v", err)
	}
	if _, err := ReadPlayers(dir, propertiesFile); err == nil {
		t.Error("players read from a pipe")
	}
}

func TestWarnings(t *testing.T) {
	defer testid.Line(t, "df5yr")
	_, dir := newServer(t)
	if w := Warnings(dir); len(w) != 1 || !strings.Contains(w[0], "whitelist is empty") {
		t.Errorf("no files: %q", w)
	}
	put(t, dir, "whitelist.json", "[]", 0o640)
	if w := Warnings(dir); len(w) != 1 || !strings.Contains(w[0], "whitelist is empty") {
		t.Errorf("empty list: %q", w)
	}
	put(t, dir, "whitelist.json", `[{"uuid": "AA", "name": "Alex"}, {"name": "Steve"}]`, 0o640)
	put(t, dir, "ops.json", `[{"uuid": "aa", "name": "Alex2"}, {"name": "steve"}]`, 0o640)
	if w := Warnings(dir); len(w) != 0 {
		t.Errorf("2 listed ops: %q", w)
	}
	put(t, dir, "ops.json", `[{"uuid": "aa", "name": "Alex"}, {"name": "steve"}, {"uuid": "bb", "name": "Mal\u001b[2Jory"}]`, 0o640)
	want := []string{
		"there are 3 ops (Alex, steve, Mal[2Jory); each one can change anything, so keep it to 2 or fewer",
		"op Mal[2Jory isn't on the whitelist",
	}
	if w := Warnings(dir); !reflect.DeepEqual(w, want) {
		t.Errorf("3 ops: %q\nwant %q", w, want)
	}
	put(t, dir, "ops.json", "not json", 0o640)
	if w := Warnings(dir); len(w) != 1 || !strings.Contains(w[0], "can't read the ops") {
		t.Errorf("bad ops.json: %q", w)
	}
}

const geyserConfig = "plugins/Geyser-Spigot/config.yml"

// Trimmed from the layout Geyser 2.12.1 writes.
var geyserNow = strings.Join([]string{
	"# Geyser configuration",
	"",
	"# Network settings for the Bedrock listener",
	"bedrock:",
	"  # The IP address that Geyser will bind on to listen for incoming Bedrock connections.",
	"  address: 0.0.0.0",
	"  port: 19132",
	"  transport: nethernet",
	"# Network settings for the Java server connection",
	"java:",
	"  # Can be \"floodgate\" (see https://wiki.geysermc.org/floodgate/), \"online\", or \"offline\".",
	"  auth-type: online",
	"motd:",
	"  primary-motd: Geyser",
	"config-version: 12",
	"",
}, "\n")

// With bedrock on, Geyser's listener follows bindIP and bedrockPort, and
// Bedrock logins go through Floodgate. A fix keeps the comments.
func TestGeyserConfig(t *testing.T) {
	defer testid.Line(t, "df5yv")
	cfg, dir := newServer(t)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.BindIP, cfg.BedrockPort = "192.168.1.123", 19133
	put(t, dir, propertiesFile, "", 0o640)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	put(t, dir, geyserConfig, geyserNow, 0o640)
	problems, err := Check(cfg)
	want := []string{geyserConfig + " bedrock.address", geyserConfig + " bedrock.port", geyserConfig + " java.auth-type"}
	if err != nil || !reflect.DeepEqual(keys(problems), want) {
		t.Fatalf("check: %v, %q; want %q", err, keys(problems), want)
	}
	if got := problems[2].String(); got != geyserConfig+`: java.auth-type is "online", and should be "floodgate"` {
		t.Errorf("String() = %q", got)
	}
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, geyserConfig))
	wantText := strings.NewReplacer("address: 0.0.0.0", "address: 192.168.1.123", "port: 19132", "port: 19133", "auth-type: online", "auth-type: floodgate").Replace(geyserNow)
	if string(b) != wantText {
		t.Errorf("config.yml:\n%s", b)
	}
	if problems, err := Check(cfg); err != nil || len(problems) != 0 {
		t.Errorf("after fix: %v, %q", err, keys(problems))
	}

	// All addresses is Geyser's 0.0.0.0, and an IPv6 address gets quoted.
	cfg.BindIP = ""
	put(t, dir, propertiesFile, "", 0o640)
	put(t, dir, geyserConfig, strings.Replace(geyserNow, "auth-type: online", "auth-type: Floodgate", 1), 0o640)
	cfg.BedrockPort = 19132
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, geyserConfig)); !strings.Contains(string(b), "auth-type: Floodgate") {
		t.Errorf("all addresses changed a safe file:\n%s", b)
	}
	cfg.BindIP = "::1"
	put(t, dir, propertiesFile, "", 0o640)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, geyserConfig)); !strings.Contains(string(b), `address: "::1"`) {
		t.Errorf("IPv6 address:\n%s", b)
	}
	if problems, err := Check(cfg); err != nil || len(problems) != 0 {
		t.Errorf("after IPv6 fix: %v, %q", err, keys(problems))
	}
}

// auth-type is looked for where the file's own layout keeps it, and a missing
// key is added under its section.
func TestGeyserLayouts(t *testing.T) {
	defer testid.Line(t, "df5yw")
	cfg, dir := newServer(t)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ text, key string }{
		{"bedrock:\n  address: 0.0.0.0\n  port: 19132\nremote:\n  address: auto\n  auth-type: online\nconfig-version: 4\n", "remote.auth-type"},
		{"bedrock:\n  address: 0.0.0.0\n  port: 19132\nauth-type: online\n", "auth-type"},
		{"bedrock:\n  address: 0.0.0.0\n  port: 19132\nremote:\n  auth-type: offline\njava:\n  auth-type: online\nconfig-version: 5\n", "java.auth-type"},
	} {
		put(t, dir, geyserConfig, c.text, 0o640)
		problems, err := Check(cfg)
		if err != nil || !reflect.DeepEqual(keys(problems), []string{geyserConfig + " " + c.key}) {
			t.Errorf("%q: %v, %q; want %s", c.text, err, keys(problems), c.key)
		}
	}

	put(t, dir, geyserConfig, "bedrock:\n    port: 19132 # udp\n\n    # comment\nmotd:\n  primary-motd: hi\nconfig-version: 12\n\n", 0o640)
	problems, err := Check(cfg)
	if err != nil || len(problems) != 2 || !problems[0].Missing || !problems[1].Missing {
		t.Fatalf("missing keys: %v, %+v", err, problems)
	}
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, geyserConfig))
	want := "bedrock:\n    port: 19132 # udp\n    address: 0.0.0.0\n\n    # comment\nmotd:\n  primary-motd: hi\nconfig-version: 12\njava:\n  auth-type: floodgate\n\n"
	if string(b) != want {
		t.Errorf("inserted:\n%s\nwant:\n%s", b, want)
	}
	if problems, err := Check(cfg); err != nil || len(problems) != 0 {
		t.Errorf("after fix: %v, %q", err, keys(problems))
	}
}

// No Geyser config yet, or bedrock off, is fine. A linked Geyser folder isn't.
func TestGeyserSkipped(t *testing.T) {
	defer testid.Line(t, "df5yx")
	cfg, dir := newServer(t)
	if _, err := Fix(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "plugins", "Geyser-Spigot"), 0o700); err != nil {
		t.Fatal(err)
	}
	if problems, err := Check(cfg); err != nil || len(problems) != 0 {
		t.Errorf("no config.yml: %v, %q", err, keys(problems))
	}
	put(t, dir, geyserConfig, geyserNow, 0o640)
	cfg.Bedrock = false
	if problems, err := Check(cfg); err != nil || len(problems) != 0 {
		t.Errorf("bedrock off: %v, %q", err, keys(problems))
	}

	cfg.Bedrock = true
	outside := t.TempDir()
	put(t, outside, "config.yml", geyserNow, 0o640)
	if err := os.RemoveAll(filepath.Join(dir, "plugins", "Geyser-Spigot")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "plugins", "Geyser-Spigot")); err != nil {
		t.Fatal(err)
	}
	if _, err := Fix(cfg); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("linked Geyser folder: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "config.yml")); string(b) != geyserNow {
		t.Error("fix wrote through the link")
	}
}
