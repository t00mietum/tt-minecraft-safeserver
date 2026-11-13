// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"encoding/json"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// The reply comes from a model or a command the operator set up, so any
// text at all. What Check passes on reaches the terminal and the downloader.
func FuzzParse(f *testing.F) {
	defer testid.Line(f, "df787")
	f.Add(`{"jars":[]}`)
	f.Add("Sure:\n```json\n{\"jars\":[{\"jar\":\"plugins/Chunky.jar\",\"version\":\"1.4.40\",\"url\":\"https://hangarcdn.papermc.io/x/Chunky.jar\",\"sha256\":\"" + sum256 + "\",\"hashFrom\":\"https://hangar.papermc.io/x\"}]}\n```")
	f.Add(`{"jars": null} {"jars":[{"jar":"plugins/LuckPerms-Bukkit.jar","notes":"\u202eevil\u001b[2J"}]}`)
	f.Add(`{"jars":[{"jar":"../../etc/passwd","url":"http://127.0.0.1/","sha512":"` + sum512 + `"}]}`)
	f.Add(`{{{{"jars"`)
	f.Fuzz(func(t *testing.T, reply string) {
		a, err := Parse(reply)
		if err != nil {
			return
		}
		js, err := json.Marshal(a)
		if err != nil {
			t.Fatalf("marshal %#v: %v", a, err)
		}
		if again, err := Parse(string(js)); err != nil || !reflect.DeepEqual(again, a) {
			t.Fatalf("answer doesn't read back from its own JSON: %v\n%#v\n%#v", err, a, again)
		}
		c := Check(a, asked, hosts)
		if n := len(c.Updates) + len(c.Current) + len(c.Refused); n > len(a.Jars) {
			t.Fatalf("%d suggestions out of %d", n, len(a.Jars))
		}
		var shown []Suggestion
		shown = append(shown, c.Updates...)
		shown = append(shown, c.Current...)
		for _, r := range c.Refused {
			shown = append(shown, r.Suggestion)
		}
		for _, s := range shown {
			for _, v := range []string{s.Jar, s.Version, s.URL, s.SHA256, s.SHA512, s.HashFrom, s.Notes} {
				if !terminalSafe(v) {
					t.Fatalf("unsafe text passed on: %q", v)
				}
			}
		}
		for _, s := range c.Updates {
			if s.Version == "" || checkAddress(s.URL, hosts) != nil || (s.HashFrom != "" && checkAddress(s.HashFrom, hosts) != nil) {
				t.Fatalf("update passed: %#v", s)
			}
		}
	})
}

func terminalSafe(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return false
		}
	}
	return true
}
