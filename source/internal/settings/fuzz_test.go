// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// server.properties is kept line for line, and a value set here reads back
// the same through the parser, whatever was in the file.
func FuzzProperties(f *testing.F) {
	defer testid.Line(f, "df78b")
	f.Add([]byte("#Minecraft server properties\nwhite-list=false\r\nserver-ip=\n"), "::1")
	f.Add([]byte("motd=A \\\n    long one\\\\\nkey\\ with\\:colon = v\\u00e9\n! bang\n"), " lead\tand\\trail\\")
	f.Add([]byte("server-ip\\\n"), "\f\u00e9\n=")
	f.Add([]byte("a\\u12"), "#!:")
	f.Fuzz(func(t *testing.T, text []byte, value string) {
		p := parseProperties(text)
		want := text
		if len(text) > 0 && text[len(text)-1] != '\n' {
			want = append(append([]byte(nil), text...), '\n')
		}
		if got := p.bytes(); !bytes.Equal(got, want) {
			t.Fatalf("not kept line for line:\n%q\n%q", text, got)
		}
		// Every value set here comes from a checked setting, so it's text.
		if !utf8.ValidString(value) {
			return
		}
		p.set("server-ip", value)
		if got, _ := parseProperties(p.bytes()).get("server-ip"); got != value {
			t.Fatalf("set %q, read back %q from %q", value, got, p.bytes())
		}
	})
}

// Fix on any server tree leaves server.properties safe and doesn't panic.
// When it passes, Check finds nothing. It fails only on a YAML file it can't
// read or fix in place, and leaves that file as it was.
func FuzzFix(f *testing.F) {
	defer testid.Line(f, "df78c")
	f.Add("white-list=false\n", "bedrock:\n  port: 19132\n", "settings:\n  bungeecord: true\n")
	f.Add("enforce-secure-profile\\\n", "bedrock: [\n", "\t- x\n  :\n")
	f.Add("", "java:\n  auth-type: online\n# c\n", "")
	f.Add("", "bedrock: {port: 1}\njava:\n  auth-type: online\n  auth-type: floodgate\n", "settings:\n  bungeecord: false\n  bungeecord: true\n")
	f.Add("", "a: &a\n  port: 2\nbedrock:\n  <<: *a\n", "s: &s\n  bungeecord: on\nsettings: *s\n")
	f.Fuzz(func(t *testing.T, props, geyser, spigot string) {
		cfg, dir := newServer(t)
		files := map[string]string{"plugins/Geyser-Spigot/config.yml": geyser, "spigot.yml": spigot}
		put(t, dir, propertiesFile, props, 0o660)
		for name, text := range files {
			put(t, dir, name, text, 0o660)
		}
		_, fixErr := Fix(cfg)
		problems, err := Check(cfg)
		if fixErr == nil && (err != nil || len(problems) != 0) {
			t.Fatalf("fix passed, then check: %v, %v", err, problems)
		}
		for _, p := range problems {
			if p.File == propertiesFile {
				t.Fatalf("still unsafe after fix: %s\nfrom %q", p, props)
			}
		}
		if fixErr == nil {
			return
		}
		for _, line := range strings.Split(fixErr.Error(), "\n") {
			name, _, _ := strings.Cut(line, ": ")
			text, ok := files[name]
			if !ok {
				t.Fatalf("fix: %v", fixErr)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != text {
				t.Fatalf("%s changed by a fix that failed: %v", name, fixErr)
			}
		}
	})
}
