// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// Any text in the config file gives warnings at worst, never an error or a
// panic, and a setting that didn't take leaves the default.
func FuzzRead(f *testing.F) {
	defer testid.Line(f, "df788")
	f.Add("runMode: native\ngamePort: 25565\nheapMax: 4GB\n")
	f.Add("allowFrom: [ 192.0.2.0/24, \"2001:db8::/32\" ]\nagent: { kind: command, command: \"/bin/true\" }\n")
	f.Add("gamePort: 99999\ngamePort: 1\nrunMode(0): x\n\x00\xff{{\n")
	f.Add("# shcl\nserverDir: \"/srv/mc\"\negress: [ web, ]\nheapMax: 4G\n")
	f.Fuzz(func(t *testing.T, text string) {
		path := filepath.Join(t.TempDir(), "tt-minecraftd.shcl")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		c := Defaults()
		warns, err := Read(path, &c)
		if err != nil {
			t.Fatalf("Read: %v (warnings %v)", err, warns)
		}
		if c.GamePort < 1 || c.GamePort > 65535 || c.BedrockPort < 1 || c.BedrockPort > 65535 {
			t.Fatalf("ports %d and %d taken", c.GamePort, c.BedrockPort)
		}
	})
}
