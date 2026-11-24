// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A jar in plugins/ that can't be read, and isn't Geyser's or Floodgate's,
// doesn't stop GeyserSources, which setup --adopt and repair's plugin
// sources step share (backlog 2026112110300182).
func TestGeyserSourcesUnreadable(t *testing.T) {
	defer testid.Line(t, "1a4z668")
	dir := t.TempDir()
	plug := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(filepath.Join(plug, "Folder.jar"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere.jar", filepath.Join(plug, "Gone.jar")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(plug, "Geyser-Spigot.jar")) // #nosec G304 -- the test's own file
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("plugin.yml")
	if err == nil {
		_, err = w.Write([]byte("name: Geyser-Spigot\nversion: 2.12.1\n"))
	}
	if err == nil {
		err = z.Close()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	// Root reads it anyway, so only the other cases run there.
	locked := os.Geteuid() != 0
	if locked {
		if err := os.WriteFile(filepath.Join(plug, "Locked.jar"), []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	jars, err := GeyserSources(dir, nil, filepath.Join(dir, "plugins.shcl"), func(p string) string { return "https://example.com/" + p })
	if err != nil || len(jars) != 1 || jars[0].Name != "plugins/Geyser-Spigot.jar" || jars[0].Source != "https://example.com/geyser" {
		t.Fatalf("locked jar %v: got %+v, %v", locked, jars, err)
	}
}
