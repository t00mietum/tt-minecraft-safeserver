// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func put(t *testing.T, file, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Jars covers plugins, Geyser extensions, and the build paper.jar points to.
func TestJars(t *testing.T) {
	defer testid.Line(t, "df5z0")
	dir := t.TempDir()
	put(t, filepath.Join(dir, "plugins", "a.jar"), "a")
	put(t, filepath.Join(dir, "plugins", "Geyser-Spigot", "extensions", "x.jar"), "x")
	put(t, filepath.Join(dir, "plugins", "Geyser-Spigot", "config.yml"), "")
	put(t, filepath.Join(dir, "builds", "paper-26.3-244.jar"), "paper")
	if err := os.Symlink("builds/paper-26.3-244.jar", filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	jars, err := Jars(dir)
	var names []string
	for _, j := range jars {
		names = append(names, j.Name)
	}
	want := []string{"plugins/a.jar", "plugins/Geyser-Spigot/extensions/x.jar", "builds/paper-26.3-244.jar"}
	if err != nil || !reflect.DeepEqual(names, want) {
		t.Errorf("Jars = %q, %v; want %q", names, err, want)
	}

	c, err := CheckJars(dir, []Approved{{Name: "plugins/a.jar", SHA256: sum("a")}, {Name: "plugins/Geyser-Spigot/extensions/gone.jar"}, {Name: "paper-old.jar"}}, &Sums{})
	if err != nil || c.Jars != 3 || !reflect.DeepEqual(c.New, want[1:]) || !reflect.DeepEqual(c.Missing, []string{"plugins/Geyser-Spigot/extensions/gone.jar"}) {
		t.Errorf("CheckJars = %+v, %v", c, err)
	}

	// A paper.jar outside the tree can't be named on the list.
	outside := filepath.Join(t.TempDir(), "paper.jar")
	put(t, outside, "paper")
	if err := os.Remove(filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if _, err := Jars(dir); err == nil || !strings.Contains(err.Error(), "can't be approved") {
		t.Errorf("outside: err %v", err)
	}
}

func sum(text string) string {
	s := sha256.Sum256([]byte(text))
	return hex.EncodeToString(s[:])
}

func makeJar(t *testing.T, file string, inside map[string]string) {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, text := range inside {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	put(t, file, b.String())
}

// Describe shows what a jar says it is, without its escape codes, and a jar
// that isn't a zip still gets its hash.
func TestDescribe(t *testing.T) {
	defer testid.Line(t, "df5z1")
	dir := t.TempDir()
	geyser := filepath.Join(dir, "plugins", "Geyser-Spigot.jar")
	makeJar(t, geyser, map[string]string{
		"plugin.yml":       "main: org.geysermc.Thing\nname: Geyser-Spigot\nversion: '2.12.1'\n",
		"paper-plugin.yml": "name: \"Geyser\x1b[2J\"\nversion: 2.12.1-b1\napi-version: '1.21'\n",
	})
	mod := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	if err := os.Chtimes(geyser, mod, mod); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(geyser)
	info, err := Describe(Jar{"plugins/Geyser-Spigot.jar", geyser})
	if err != nil || info.About != "Geyser[2J 2.12.1-b1" || info.SHA256 != sum(string(raw)) || info.Size != int64(len(raw)) || !info.Mod.Equal(mod) {
		t.Errorf("Describe = %+v, %v", info, err)
	}

	ext := filepath.Join(dir, "plugins", "Geyser-Spigot", "extensions", "x.jar")
	makeJar(t, ext, map[string]string{"extension.yml": "id: x\nname: Thing\n"})
	if info, err := Describe(Jar{"x", ext}); err != nil || info.About != "Thing" {
		t.Errorf("extension: %+v, %v", info, err)
	}
	put(t, ext, "not a zip")
	if info, err := Describe(Jar{"x", ext}); err != nil || info.About != "" || info.SHA256 != sum("not a zip") {
		t.Errorf("not a zip: %+v, %v", info, err)
	}
}

// Approve adds and updates hashes, and leaves the rest of the file alone.
func TestApprove(t *testing.T) {
	defer testid.Line(t, "df5z2")
	list := filepath.Join(t.TempDir(), "plugins.shcl")
	odd := "plugins/floodgate (1).jar"
	if err := Approve(list, []Info{{Jar: Jar{Name: "plugins/a.jar"}, SHA256: sumA}, {Jar: Jar{Name: odd}, SHA256: sumB}}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(list); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("new list: %v, %v", fi, err)
	}
	jars, warns, err := ReadApproved(list)
	want := []Approved{{Name: "plugins/a.jar", SHA256: sumA, Line: 1}, {Name: odd, SHA256: sumB, Line: 4}}
	if err != nil || len(warns) != 0 || !reflect.DeepEqual(jars, want) {
		t.Errorf("new list reads %+v, %v, %v", jars, warns, err)
	}

	text := "## approved by hand\nNoteToSelf: Keep\njar: plugins/Geyser-Spigot.jar\n\tsha256: " + sumA + "  # 2.12.0\n\tsource: https://download.geysermc.org/x\n"
	if err := os.WriteFile(list, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(list, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Approve(list, []Info{{Jar: Jar{Name: "plugins/Geyser-Spigot.jar"}, SHA256: sumB}, {Jar: Jar{Name: "paper-26.3-245.jar"}, SHA256: sumA}}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(list)
	if !strings.HasPrefix(string(got), "## approved by hand\nNoteToSelf: Keep\njar: plugins/Geyser-Spigot.jar\n\tsha256: "+sumB) || !strings.Contains(string(got), "geysermc.org/x") {
		t.Errorf("comments or source lost:\n%s", got)
	}
	if fi, err := os.Stat(list); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("mode changed: %v, %v", fi, err)
	}
	jars, _, _ = ReadApproved(list)
	if len(jars) != 2 || jars[0].SHA256 != sumB || jars[0].Source == "" || jars[1].Name != "paper-26.3-245.jar" {
		t.Errorf("updated list reads %+v", jars)
	}

	if err := Approve(list, []Info{{Jar: Jar{Name: "../evil.jar"}, SHA256: sumA}}); err == nil {
		t.Error("approved a path outside the tree")
	}
}

// Refuse moves a plugin out of the way, and never paper.
func TestRefuse(t *testing.T) {
	defer testid.Line(t, "df5z3")
	dir := t.TempDir()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.Local)
	for i := range 2 {
		put(t, filepath.Join(dir, "plugins", "bad.jar"), "bad")
		dest, err := Refuse(dir, Jar{Name: "plugins/bad.jar"}, now)
		want := filepath.Join(dir, RefusedDir, "bad.jar")
		if i == 1 {
			want += ".20261111-120000"
		}
		if err != nil || dest != want {
			t.Errorf("Refuse = %q, %v; want %q", dest, err, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "bad.jar")); err == nil {
		t.Error("still in plugins/")
	}
	put(t, filepath.Join(dir, "paper-26.3-244.jar"), "paper")
	if _, err := Refuse(dir, Jar{Name: "paper-26.3-244.jar"}, now); err == nil {
		t.Error("moved paper")
	}
}
