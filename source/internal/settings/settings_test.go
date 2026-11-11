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
