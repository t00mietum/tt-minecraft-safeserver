// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"bytes"
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

// Fix on any server tree leaves server.properties safe, and doesn't fail or
// panic on the YAML files it edits.
func FuzzFix(f *testing.F) {
	defer testid.Line(f, "df78c")
	f.Add("white-list=false\n", "bedrock:\n  port: 19132\n", "settings:\n  bungeecord: true\n")
	f.Add("enforce-secure-profile\\\n", "bedrock: [\n", "\t- x\n  :\n")
	f.Add("", "java:\n  auth-type: online\n# c\n", "")
	f.Fuzz(func(t *testing.T, props, geyser, spigot string) {
		cfg, dir := newServer(t)
		put(t, dir, propertiesFile, props, 0o660)
		put(t, dir, "plugins/Geyser-Spigot/config.yml", geyser, 0o660)
		put(t, dir, "spigot.yml", spigot, 0o660)
		if _, err := Fix(cfg); err != nil {
			t.Fatalf("fix: %v", err)
		}
		problems, err := Check(cfg)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		for _, p := range problems {
			if p.File == propertiesFile {
				t.Fatalf("still unsafe after fix: %s\nfrom %q", p, props)
			}
		}
	})
}
