// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestCheckJars(t *testing.T) {
	defer testid.Line(t, "df5ye")
	dir := t.TempDir()
	put := func(name, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(text string) string {
		s := sha256.Sum256([]byte(text))
		return hex.EncodeToString(s[:])
	}
	put("plugins/a.jar", "a")
	put("plugins/b.jar", "b")
	put("plugins/notes.txt", "not a jar")
	approved := []Approved{
		{Name: "plugins/a.jar", SHA256: hash("a")},
		{Name: "plugins/b.jar", SHA256: hash("old b")},
		{Name: "plugins/gone.jar", SHA256: hash("gone")},
		{Name: "paper-26.3-244.jar", SHA256: hash("paper")},
	}
	var sums Sums
	c, err := CheckJars(dir, approved, &sums)
	want := Check{Jars: 2, Changed: []string{"plugins/b.jar"}, Missing: []string{"plugins/gone.jar"}}
	if err != nil || !reflect.DeepEqual(c, want) || c.OK() {
		t.Errorf("CheckJars = %+v, %v; want %+v", c, err, want)
	}

	// A jar swapped for one of another size is hashed again, cache or not.
	put("plugins/a.jar", "evil a")
	put("plugins/c.jar", "c")
	c, err = CheckJars(dir, approved, &sums)
	if err != nil || !reflect.DeepEqual(c.Changed, []string{"plugins/a.jar", "plugins/b.jar"}) || !reflect.DeepEqual(c.New, []string{"plugins/c.jar"}) {
		t.Errorf("after the swap: %+v, %v", c, err)
	}

	// Same size, new time: hashed again too.
	put("plugins/a.jar", "a")
	approved[1].SHA256 = hash("b")
	if c, err := CheckJars(dir, approved[:2], &sums); err != nil || len(c.Changed) != 0 || !reflect.DeepEqual(c.New, []string{"plugins/c.jar"}) {
		t.Errorf("after the fix: %+v, %v", c, err)
	}
	put("plugins/a.jar", "z")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "plugins/a.jar"), old, old); err != nil {
		t.Fatal(err)
	}
	if c, _ := CheckJars(dir, approved[:2], &sums); !reflect.DeepEqual(c.Changed, []string{"plugins/a.jar"}) {
		t.Errorf("same size swap missed: %+v", c)
	}
}
