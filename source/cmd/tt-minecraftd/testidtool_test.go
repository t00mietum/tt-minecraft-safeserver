// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// test-id.bash run against a throwaway tree with one Go test and the given checks.bash.
func runTestID(t *testing.T, checks, goID, mode string) (string, int) {
	t.Helper()
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	tool := cicdFile(t, filepath.Join("utility", "test-id.bash"))
	root := t.TempDir()
	files := map[string]string{
		filepath.Join("source", "x", "x_test.go"):         "package x\n\nfunc TestX(t *testing.T) {\n\tdefer testid.Line(t, \"" + goID + "\")\n}\n",
		filepath.Join("cicd", "root-host", "checks.bash"): checks,
	}
	for name, text := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// #nosec G204 -- test runs the repo's own script
	out, err := exec.Command(bashPath, tool, mode, "--root", root).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestTestIDRootChecks(t *testing.T) {
	defer testid.Line(t, "df7b7")
	checks, err := os.ReadFile(cicdFile(t, filepath.Join("root-host", "checks.bash")))
	if err != nil {
		t.Fatal(err)
	}

	// The checks that run through native are the ones the old sed missed.
	t.Run("native checks are listed", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "zzzzz", "--list")
		for _, id := range []string{"df680", "df68j", "df68b", "df68c", "df68d"} {
			if code != 0 || !strings.Contains("\n"+out, "\n"+id+" ") {
				t.Errorf("code %d, %s not listed\n%s", code, id, out)
			}
		}
	})

	t.Run("a Go test reusing a native check's ID fails", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "df68b", "--check")
		if code != 1 || !strings.Contains(out, "ID used twice: df68b") {
			t.Errorf("code %d, want df68b as a repeat\n%s", code, out)
		}
	})

	// Both through check, and once through a wrapper that hands its arguments on.
	t.Run("a check ID used twice in checks.bash fails", func(t *testing.T) {
		head := "check() { \"${@}\"; }\nwrap() {\n\tcheck \"${@}\"\n}\ncheck aaaaa \"one\" true\n"
		for _, second := range []string{"check aaaaa \"two\" true\n", "wrap aaaaa \"two\" true\n"} {
			out, code := runTestID(t, head+second, "zzzzz", "--check")
			if code != 1 || !strings.Contains(out, "ID used twice: aaaaa") {
				t.Errorf("%scode %d, want aaaaa as a repeat\n%s", second, code, out)
			}
		}
	})

	// The control: the real file with a free Go ID is clean, so the failures above are the repeats.
	t.Run("no repeat passes", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "zzzzz", "--check")
		if code != 0 {
			t.Errorf("code %d, want clean\n%s", code, out)
		}
	})
}
