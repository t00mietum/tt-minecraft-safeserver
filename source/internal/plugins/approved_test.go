// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

const (
	sumA = "a3f1c0ffee00000000000000000000000000000000000000000000000000beef"
	sumB = "0000000000000000000000000000000000000000000000000000000000000001"
)

func writeList(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugins.shcl")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadApprovedList(t *testing.T) {
	defer testid.Line(t, "df5x3")
	text := "## approved jars\n" +
		"jar: plugins/Geyser-Spigot.jar\n" +
		"\tsha256: " + strings.ToUpper(sumA) + "\n" +
		"\tsource: https://download.geysermc.org/v2/projects/geyser/versions/latest/builds/latest/downloads/spigot\n" +
		"jar: paper-26.3-244.jar\n" +
		"\tsha256: " + sumB + "\n"
	jars, warns, err := ReadApproved(writeList(t, text))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err %v, warnings %v", err, warns)
	}
	want := []Approved{
		{"plugins/Geyser-Spigot.jar", sumA, "https://download.geysermc.org/v2/projects/geyser/versions/latest/builds/latest/downloads/spigot", 2},
		{"paper-26.3-244.jar", sumB, "", 5},
	}
	if !reflect.DeepEqual(jars, want) {
		t.Errorf("got  %+v\nwant %+v", jars, want)
	}
}

func TestBadEntriesAreLeftOut(t *testing.T) {
	defer testid.Line(t, "df5x4")
	text := "jar: plugins/good.jar\n\tsha256: " + sumA + "\n" + // 1
		"jar: plugins/nosum.jar\n" + // 3
		"jar: plugins/short.jar\n\tsha256: abc123\n" + // 4
		"jar: ../evil.jar\n\tsha256: " + sumA + "\n" + // 6
		"jar: /etc/evil.jar\n\tsha256: " + sumA + "\n" + // 8
		"jar: plugins/good.jar\n\tsha256: " + sumB + "\n" + // 10
		"jar: plugins/http.jar\n\tsha256: " + sumB + "\n\tsource: http://example.com/x.jar\n" + // 12
		"jar: plugins/twice.jar\n\tsha256: " + sumA + "\n\tsha256: " + sumB + "\n" + // 15
		"jar: plugins/extra.jar\n\tsha256: " + sumB + "\n\tversion: 2\n" + // 18
		"jars: plugins/typo.jar\n" + // 21
		"jar: plugins/same.jar\n\tsha256: " + sumB + "\n" + // 22
		"jar: plugins/same.jar\n\tsha256: " + sumB + "\n" // 24
	jars, warns, err := ReadApproved(writeList(t, text))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, j := range jars {
		names = append(names, j.Name+"@"+j.Source)
	}
	if got := strings.Join(names, " "); got != "plugins/http.jar@ plugins/extra.jar@ plugins/same.jar@" {
		t.Errorf("kept %s", got)
	}
	wantLines := map[int]string{
		1:  "more than one sha256",
		3:  "has no sha256",
		5:  "isn't 64 hex digits",
		6:  "isn't a path under the server folder",
		8:  "isn't a path under the server folder",
		14: "source isn't an https URL",
		15: "more than one sha256",
		20: `unknown key "version"`,
		21: `unknown key "jars"`,
	}
	for _, w := range warns {
		if says, ok := wantLines[w.Line]; ok && strings.Contains(w.Text, says) {
			delete(wantLines, w.Line)
		}
	}
	for line, says := range wantLines {
		t.Errorf("no warning on line %d saying %q; got %v", line, says, warns)
	}
}

func TestUnreadableListIsAnError(t *testing.T) {
	defer testid.Line(t, "df5x5")
	_, _, err := ReadApproved(filepath.Join(t.TempDir(), "plugins.shcl"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v", err)
	}
}
