// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// Check 13: with startAtBoot on, the unit matches the config and is enabled,
// and is never started. Off, it's disabled and removed, and a server it runs
// is left alone.
func TestBootCheck(t *testing.T) {
	defer testid.Line(t, "df78r00")
	enabled := "disabled\n"
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) { return []byte(enabled), nil })
	h.Cfg.StartAtBoot = true
	unit := boot.Unit(h.Cfg, h.ConfigPath, boot.LocalBin)

	hasAll(t, "new", look(t, h, BootCheck), "is missing or doesn't match the config [fix]", "tt-minecraftd.service isn't enabled [fix]")
	want := []string{
		"sudo install -D -m 0644 -o root -g root -T /dev/stdin " + h.BootFile,
		"sudo systemctl daemon-reload",
		"sudo systemctl enable tt-minecraftd.service",
	}
	if got := fixAll(t, h, f, BootCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("fixes ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if string(f.Ran[0].Stdin) != unit || !strings.Contains(unit, "--config ") {
		t.Errorf("wrote %q", f.Ran[0].Stdin)
	}

	if err := os.WriteFile(h.BootFile, []byte(unit), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled = "enabled\n"
	if got := look(t, h, BootCheck); len(got) != 0 {
		t.Errorf("all in place: %q", got)
	}
	// A config change, such as Docker mode, rewrites it.
	h.Cfg.RunMode = "podman"
	if got := fixAll(t, h, f, BootCheck); !reflect.DeepEqual(got, want[:2]) {
		t.Errorf("after a config change: %q", got)
	}

	h.Cfg.StartAtBoot = false
	want = []string{"sudo systemctl disable tt-minecraftd.service", "sudo rm -f -- " + h.BootFile, "sudo systemctl daemon-reload"}
	if got := fixAll(t, h, f, BootCheck); !reflect.DeepEqual(got, want) {
		t.Errorf("off: ran %q, want %q", got, want)
	}
	if err := os.Remove(h.BootFile); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, BootCheck); len(got) != 0 {
		t.Errorf("off and gone: %q", got)
	}
	if !slicesHas(After, BootCheck.Name) {
		t.Error("repair doesn't run the boot check")
	}
}

func slicesHas(checks []Check, name string) bool {
	for _, c := range checks {
		if c.Name == name {
			return true
		}
	}
	return false
}

// There's one boot unit. A second config's repair leaves the one that runs
// the first alone, whichever way its startAtBoot is, and says whose it is.
func TestBootOtherConfig(t *testing.T) {
	defer testid.Line(t, "df7a800")
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) { return []byte("enabled\n"), nil })
	first := h.ConfigPath
	h.Cfg.StartAtBoot = true
	if err := os.WriteFile(h.BootFile, []byte(boot.Unit(h.Cfg, first, boot.PkgBin)), 0o600); err != nil {
		t.Fatal(err)
	}
	h.ConfigPath = filepath.Join(filepath.Dir(first), "second.shcl")
	h.Cfg.RunMode = "podman"
	got := look(t, h, BootCheck)
	if len(got) != 1 || !strings.Contains(got[0], "runs the config at "+first+"; there's one boot unit") || strings.HasSuffix(got[0], "[fix]") {
		t.Errorf("on: %q", got)
	}
	h.Cfg.StartAtBoot = false
	var lines []string
	Run(context.Background(), h, []Check{BootCheck}, false, func(state, text string) { lines = append(lines, state+" "+text) })
	if want := []string{"OK boot unit: tt-minecraftd.service runs the config at " + first + ", so it's left alone"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("off: %q", lines)
	}
	for _, on := range []bool{true, false} {
		h.Cfg.StartAtBoot = on
		if ran := fixAll(t, h, f, BootCheck); len(ran) != 0 {
			t.Errorf("startAtBoot %v changed the first config's unit: %q", on, ran)
		}
	}

	// The same file by another name is still this config's.
	h.ConfigPath = filepath.Join(filepath.Dir(first), ".", filepath.Base(first))
	if ran := fixAll(t, h, f, BootCheck); len(ran) != 3 {
		t.Errorf("first config, off: %q", ran)
	}
	// So is a unit with no ExecStart it wrote.
	h.ConfigPath = filepath.Join(filepath.Dir(first), "second.shcl")
	if err := os.WriteFile(h.BootFile, []byte("[Service]\nExecStart=/bin/true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ran := fixAll(t, h, f, BootCheck); len(ran) != 3 {
		t.Errorf("a unit it can't place: %q", ran)
	}
}

// A package installed after setup's copy moves the unit to the package's
// binary, and the copy, which comes first on PATH, is moved to the
// old-builds folder. Check 15. See design.md, Boot.
func TestPackageOverCopy(t *testing.T) {
	defer testid.Line(t, "df7av00")
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) { return []byte("enabled\n"), nil })
	h.Cfg.StartAtBoot = true
	if err := os.WriteFile(h.BootFile, []byte(boot.Unit(h.Cfg, h.ConfigPath, boot.LocalBin)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.LocalBin, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 11, 19, 11, 12, 57, 0, time.Local)
	if err := os.Chtimes(h.LocalBin, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, BootCheck); len(got) != 0 {
		t.Errorf("no package, unit in place: %q", got)
	}
	if got := look(t, h, shadowCheck); len(got) != 0 {
		t.Errorf("no package, only setup's copy: %q", got)
	}

	if err := os.WriteFile(h.PkgBin, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fixAll(t, h, f, BootCheck); len(got) != 2 || !strings.Contains(string(f.Ran[0].Stdin), "\nExecStart="+boot.PkgBin+" --config ") {
		t.Errorf("package there: ran %q, wrote %q", got, f.Ran[0].Stdin)
	}
	// 2026-11-19: it said "remove it with: sudo rm", and the fix left the copy alone (2026111911125756).
	want := "/usr/local/bin/tt-minecraftd comes before the package's /usr/bin/tt-minecraftd on PATH, so a package upgrade doesn't change what runs; repair moves it to /var/lib/tt-minecraftd/old-builds/tt-minecraftd.20261119-111257 [fix]"
	if got := look(t, h, shadowCheck); !reflect.DeepEqual(got, []string{want}) {
		t.Errorf("copy over the package: %q", got)
	}
	if ran := fixAll(t, h, f, shadowCheck); len(ran) != 0 {
		t.Errorf("the move ran commands: %q", ran)
	}
	if b, err := os.ReadFile(filepath.Join(h.OldBuilds, "tt-minecraftd.20261119-111257")); err != nil || string(b) != "old" {
		t.Errorf("the copy wasn't kept: %q %v", b, err)
	}
	if got := look(t, h, shadowCheck); len(got) != 0 {
		t.Errorf("after the move: %q", got)
	}
	if !slicesHas(After, shadowCheck.Name) || After[len(After)-1].Name != shadowCheck.Name {
		t.Error("repair doesn't run the PATH check last")
	}

	// A link to the package's runs the package's.
	if err := os.Symlink(h.PkgBin, h.LocalBin); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, shadowCheck); len(got) != 0 {
		t.Errorf("a link to the package's: %q", got)
	}
	// A link to some other file is reported, with nothing to keep.
	other := filepath.Join(t.TempDir(), "tt-minecraftd")
	if err := os.WriteFile(other, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(h.LocalBin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, h.LocalBin); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, shadowCheck); len(got) != 1 || !strings.HasSuffix(got[0], "it's a link, so remove it with: sudo rm /usr/local/bin/tt-minecraftd") {
		t.Errorf("a link to another file: %q", got)
	}
	if err := os.Remove(h.LocalBin); err != nil {
		t.Fatal(err)
	}
	if got := look(t, h, shadowCheck); len(got) != 0 {
		t.Errorf("package only: %q", got)
	}
}
