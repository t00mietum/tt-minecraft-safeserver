// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	defer testid.Line(t, "df7b700")
	checks, err := os.ReadFile(cicdFile(t, filepath.Join("root-host", "checks.bash")))
	if err != nil {
		t.Fatal(err)
	}

	// The checks that run through native are the ones the old sed missed.
	t.Run("native checks are listed", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "zzzzz00", "--list")
		for _, id := range []string{"df68000", "df68j00", "df68b00", "df68c00", "df68d00"} {
			if code != 0 || !strings.Contains("\n"+out, "\n"+id+" ") {
				t.Errorf("code %d, %s not listed\n%s", code, id, out)
			}
		}
	})

	t.Run("a Go test reusing a native check's ID fails", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "df68b00", "--check")
		if code != 1 || !strings.Contains(out, "ID used twice: df68b00") {
			t.Errorf("code %d, want df68b00 as a repeat\n%s", code, out)
		}
	})

	// Both through check, and once through a wrapper that hands its arguments on.
	t.Run("a check ID used twice in checks.bash fails", func(t *testing.T) {
		head := "check() { \"${@}\"; }\nwrap() {\n\tcheck \"${@}\"\n}\ncheck aaaaa00 \"one\" true\n"
		for _, second := range []string{"check aaaaa00 \"two\" true\n", "wrap aaaaa00 \"two\" true\n"} {
			out, code := runTestID(t, head+second, "zzzzz00", "--check")
			if code != 1 || !strings.Contains(out, "ID used twice: aaaaa00") {
				t.Errorf("%scode %d, want aaaaa00 as a repeat\n%s", second, code, out)
			}
		}
	})

	// The control: the real file with a free Go ID is clean, so the failures above are the repeats.
	t.Run("no repeat passes", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "zzzzz00", "--check")
		if code != 0 {
			t.Errorf("code %d, want clean\n%s", code, out)
		}
	})
}

// The clock's minute in 5 Crockford characters, as test-id.bash --new makes it.
func clockStem(at time.Time) string {
	const digits = "0123456789abcdefghjkmnpqrstvwxyz"
	m := (at.Unix() - 946684800) / 60
	b := make([]byte, 5)
	for i := 4; i >= 0; i-- {
		b[i] = digits[m%32]
		m /= 32
	}
	return string(b)
}

func TestTestIDForm(t *testing.T) {
	defer testid.Line(t, "dfh7x00")

	t.Run("form", func(t *testing.T) {
		for id, want := range map[string]int{
			"dfh7x00": 0, "dfh7x0z": 0, "df68000": 0,
			"df680": 1, "dfh7x0": 1, "dfh7x000": 1, "dfh7xi0": 1,
		} {
			out, code := runTestID(t, "", id, "--check")
			if code != want || (want == 1 && !strings.Contains(out, "bad ID \""+id+"\"")) {
				t.Errorf("%s: code %d, want %d\n%s", id, code, want, out)
			}
		}
	})

	t.Run("root check form", func(t *testing.T) {
		out, code := runTestID(t, "check abcde00 \"seven\" true\n", "zzzzz00", "--list")
		if code != 0 || !strings.Contains("\n"+out, "\nabcde00 seven\n") {
			t.Errorf("code %d\n%s", code, out)
		}
		out, code = runTestID(t, "check abcde \"five\" true\n", "zzzzz00", "--check")
		if code != 1 || !strings.Contains(out, "bad ID \"abcde\"") {
			t.Errorf("code %d, want abcde refused\n%s", code, out)
		}
	})

	// The stem is the clock's minute, and a counter already taken that minute is skipped.
	t.Run("new", func(t *testing.T) {
		for range 3 {
			stem := clockStem(time.Now())
			out, code := runTestID(t, "check "+stem+"01 \"taken\" true\n", stem+"00", "--new")
			if stem != clockStem(time.Now()) {
				continue // the minute turned over mid-run
			}
			if got := strings.TrimSpace(out); code != 0 || got != stem+"02" {
				t.Errorf("code %d, got %q, want %s02", code, got, stem)
			}
			return
		}
		t.Skip("the minute kept turning over")
	})
}
