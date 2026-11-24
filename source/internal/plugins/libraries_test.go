// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// update lists the cache while the server may run, and a running Paper can
// take a folder away between the listing and the read.
func TestLibrariesFolderGone(t *testing.T) {
	defer testid.Line(t, "1a4q4fc")
	dir := t.TempDir()
	for _, name := range []string{"libraries/org/a/a-1.jar", "libraries/org/b/b-1.jar", "libraries/org/z/z-1.jar"} {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gone := filepath.Join(dir, "libraries", "org", "b")
	took := false
	old := walkDir
	t.Cleanup(func() { walkDir = old })
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		return old(root, func(p string, d fs.DirEntry, err error) error {
			if p == gone && !took {
				took = true
				if err := os.RemoveAll(p); err != nil {
					t.Fatal(err)
				}
			}
			return fn(p, d, err)
		})
	}
	jars, err := Libraries(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !took {
		t.Fatal("the walk never reached the folder taken away")
	}
	want := []Jar{
		{"libraries/org/a/a-1.jar", filepath.Join(dir, "libraries", "org", "a", "a-1.jar")},
		{"libraries/org/z/z-1.jar", filepath.Join(dir, "libraries", "org", "z", "z-1.jar")},
	}
	if !reflect.DeepEqual(jars, want) {
		t.Errorf("got %v, want %v", jars, want)
	}
}

// Commented out by 2026111914334569: CheckLibraries went with library pinning.

// import (
// 	"crypto/sha256"
// 	"encoding/hex"
// 	"os"
// 	"path/filepath"
// 	"reflect"
// 	"testing"

// 	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
// )

// // Paper's library cache is checked against the approved list, less the
// // files paper.jar puts back itself. Links to folders, and a link at the
// // cache folder, can never pass.
// func TestCheckLibraries(t *testing.T) {
// 	defer testid.Line(t, "1a4hxz8")
// 	dir := t.TempDir()
// 	put := func(name, text string) {
// 		t.Helper()
// 		file := filepath.Join(dir, filepath.FromSlash(name))
// 		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
// 			t.Fatal(err)
// 		}
// 		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
// 			t.Fatal(err)
// 		}
// 	}
// 	sum := func(text string) string {
// 		s := sha256.Sum256([]byte(text))
// 		return hex.EncodeToString(s[:])
// 	}
// 	own := "com/google/guava/guava/33.3.1-jre/guava-33.3.1-jre.jar"
// 	put("paper-26.3-256.jar", string(zipOf(map[string]string{
// 		"META-INF/libraries.list": sum("g") + "\tcom.google.guava:guava:33.3.1-jre\t" + own + "\nbad line\n" + sum("x") + "\tx:x:1\t../../plugins/x.jar\n",
// 	})))
// 	if err := os.Symlink("paper-26.3-256.jar", filepath.Join(dir, "paper.jar")); err != nil {
// 		t.Fatal(err)
// 	}
// 	put("libraries/"+own, "changed, and Paperclip's to fix")
// 	put("libraries/org/a/a/1/a-1.jar", "a")
// 	put("libraries/org/b/b/1/b-1.jar", "b changed")
// 	put("libraries/org/c/c/1/c-1.jar", "c")
// 	put("libraries/org/c/c/1/c-1.pom", "not loaded")
// 	put("plugins/.paper-remapped/Geyser-Spigot.jar", "remapped")
// 	put("plugins/x.jar", "a plugin, checked by Jars")
// 	put("world/datapacks/z.jar", "z")
// 	if err := os.Symlink(filepath.Join(dir, "world"), filepath.Join(dir, "libraries", "org", "w")); err != nil {
// 		t.Fatal(err)
// 	}
// 	approved := []Approved{
// 		{Name: "libraries/org/a/a/1/a-1.jar", SHA256: sum("a")},
// 		{Name: "libraries/org/b/b/1/b-1.jar", SHA256: sum("b")},
// 		{Name: "libraries/org/gone/1/gone-1.jar", SHA256: sum("gone")},
// 	}
// 	c, err := CheckLibraries(dir, approved, &Sums{})
// 	want := Check{
// 		Jars:    5,
// 		New:     []string{"libraries/org/c/c/1/c-1.jar", "libraries/org/w", "plugins/.paper-remapped/Geyser-Spigot.jar"},
// 		Changed: []string{"libraries/org/b/b/1/b-1.jar"},
// 	}
// 	if err != nil || !reflect.DeepEqual(c, want) {
// 		t.Errorf("got %+v, %v; want %+v", c, err, want)
// 	}

// 	// With no paper.jar, Paper's own count too.
// 	if err := os.Remove(filepath.Join(dir, "paper.jar")); err != nil {
// 		t.Fatal(err)
// 	}
// 	if c, err := CheckLibraries(dir, approved, &Sums{}); err != nil || len(c.New) != 4 || c.New[0] != "libraries/"+own {
// 		t.Errorf("no paper.jar: %+v, %v", c, err)
// 	}

// 	// A link at libraries/ is listed by itself and not followed, even when
// 	// it leads nowhere.
// 	if err := os.RemoveAll(filepath.Join(dir, "libraries")); err != nil {
// 		t.Fatal(err)
// 	}
// 	for _, to := range []string{filepath.Join(dir, "world"), filepath.Join(dir, "nowhere")} {
// 		_ = os.Remove(filepath.Join(dir, "libraries"))
// 		if err := os.Symlink(to, filepath.Join(dir, "libraries")); err != nil {
// 			t.Fatal(err)
// 		}
// 		c, err := CheckLibraries(dir, approved, &Sums{})
// 		if err != nil || !reflect.DeepEqual(c.New, []string{"libraries", "plugins/.paper-remapped/Geyser-Spigot.jar"}) || c.Jars != 2 {
// 			t.Errorf("libraries -> %s: %+v, %v", to, c, err)
// 		}
// 	}

// 	for name, want := range map[string]bool{
// 		"libraries/org/a/a/1/a-1.jar": true, "plugins/.paper-remapped/x.jar": true,
// 		"plugins/x.jar": false, "paper-26.3-256.jar": false, "libraries": false, "librariesx/a.jar": false,
// 	} {
// 		if IsLibrary(name) != want {
// 			t.Errorf("IsLibrary(%q) = %v", name, !want)
// 		}
// 	}
// }
