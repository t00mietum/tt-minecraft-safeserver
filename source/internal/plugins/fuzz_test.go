// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"archive/zip"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"unicode/utf8"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

var lowerHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Whatever the list says, a jar it approves has a path inside the server
// folder and a whole hash, and a source only when it's an https URL.
func FuzzReadApproved(f *testing.F) {
	defer testid.Line(f, "df789")
	f.Add("jar: plugins/Geyser-Spigot.jar\n\tsha256: " + sumA + "\n\tsource: https://download.geysermc.org/x\n")
	f.Add("jar: ../paper.jar\n\tsha256: " + sumB + "\njar: /etc/passwd\n\tsha256: " + sumB + "\n")
	f.Add("jar: plugins/a.jar\n\tsha256: " + sumA + "\njar: plugins/a.jar\n\tsha256: " + sumB + "\n\tsource: http://x\n")
	f.Add("jar(0): x\nfoo: [1,2]\n\tsha256: {\n\x00")
	f.Fuzz(func(t *testing.T, text string) {
		path := filepath.Join(t.TempDir(), "plugins.shcl")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		jars, warns, err := ReadApproved(path)
		if err != nil {
			t.Fatalf("ReadApproved: %v (warnings %v)", err, warns)
		}
		for _, j := range jars {
			if !insideServer(j.Name) || !lowerHex64.MatchString(j.SHA256) {
				t.Fatalf("approved %#v", j)
			}
			if j.Source != "" {
				if u, err := url.Parse(j.Source); err != nil || u.Scheme != "https" || u.Host == "" {
					t.Fatalf("source kept: %q", j.Source)
				}
			}
		}
	})
}

func zipOf(files map[string]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, text := range files {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(text))
	}
	_ = z.Close()
	return b.Bytes()
}

// A downloaded jar names itself. Its name and version reach the terminal, so
// they must come out as plain text of a sane length.
func FuzzClaims(f *testing.F) {
	defer testid.Line(f, "df78a")
	f.Add(zipOf(map[string]string{"plugin.yml": "name: Chunky\nversion: 1.4.40\nmain: x.Y\n"}))
	f.Add(zipOf(map[string]string{"paper-plugin.yml": "name: \"Geyser\x1b[2J\"\nversion: '2.12.1'\n"}))
	f.Add([]byte("name: LuckPerms\u202e\nversion: " + string(bytes.Repeat([]byte("\u0436"), 60)) + "\n"))
	f.Add([]byte("PK\x03\x04garbage"))
	f.Fuzz(func(t *testing.T, b []byte) {
		name, version := claims(bytes.NewReader(b), int64(len(b)))
		check := func(what, s string) {
			if !utf8.ValidString(s) || len(s) > 80+len("...") {
				t.Fatalf("%s: %q", what, s)
			}
			for _, r := range s {
				if r < 0x20 || (r >= 0x7f && r < 0xa0) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
					t.Fatalf("%s: %q", what, s)
				}
			}
		}
		check("zip name", name)
		check("zip version", version)
		// Most inputs aren't zips, so the yml reader gets them straight too.
		name, version = yamlNameVersion(bytes.NewReader(b))
		check("yml name", name)
		check("yml version", version)
	})
}
