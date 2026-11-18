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

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// Check 13: with startAtBoot on, the unit matches the config and is enabled,
// and is never started. Off, it's disabled and removed, and a server it runs
// is left alone.
func TestBootCheck(t *testing.T) {
	defer testid.Line(t, "df78r")
	enabled := "disabled\n"
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) { return []byte(enabled), nil })
	h.Cfg.StartAtBoot = true
	unit := boot.Unit(h.Cfg, h.ConfigPath)

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
	defer testid.Line(t, "df7a8")
	h, f := testHost(t, "", "", func(c host.Cmd) ([]byte, error) { return []byte("enabled\n"), nil })
	first := h.ConfigPath
	h.Cfg.StartAtBoot = true
	if err := os.WriteFile(h.BootFile, []byte(boot.Unit(h.Cfg, first)), 0o600); err != nil {
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
