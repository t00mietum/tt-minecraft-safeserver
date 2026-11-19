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
	defer testid.Line(t, "1a3jz1w")
	checks, err := os.ReadFile(cicdFile(t, filepath.Join("root-host", "checks.bash")))
	if err != nil {
		t.Fatal(err)
	}

	// The checks that run through native are the ones the old sed missed.
	t.Run("native checks are listed", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "zzzzz00", "--list")
		for _, id := range []string{"1a3fh00", "1a3fjr8", "1a3fj2c", "1a3fj5g", "1a3fj8m"} {
			if code != 0 || !strings.Contains("\n"+out, "\n"+id+" ") {
				t.Errorf("code %d, %s not listed\n%s", code, id, out)
			}
		}
	})

	t.Run("a Go test reusing a native check's ID fails", func(t *testing.T) {
		out, code := runTestID(t, string(checks), "1a3fj2c", "--check")
		if code != 1 || !strings.Contains(out, "ID used twice: 1a3fj2c") {
			t.Errorf("code %d, want 1a3fj2c as a repeat\n%s", code, out)
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

// The clock's minute times 100 plus the counter, in 7 Crockford characters, as test-id.bash
// --new makes it.
func clockID(at time.Time, counter int64) string {
	const digits = "0123456789abcdefghjkmnpqrstvwxyz"
	v := (at.Unix()-946684800)/60*100 + counter
	b := make([]byte, 7)
	for i := 6; i >= 0; i-- {
		b[i] = digits[v%32]
		v /= 32
	}
	return string(b)
}

func TestTestIDForm(t *testing.T) {
	defer testid.Line(t, "1a4hwpm")

	t.Run("form", func(t *testing.T) {
		for id, want := range map[string]int{
			"1a4hwpm": 0, "dfh7x0z": 0, "1a3fh00": 0,
			"df680": 1, "1a4hwp": 1, "1a4hwpm0": 1, "1a4hwim": 1,
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

	// The minute times 100 is the base, the counter adds to it at base 10, and a counter already
	// taken that minute is skipped.
	t.Run("new", func(t *testing.T) {
		for range 3 {
			now := time.Now()
			out, code := runTestID(t, "check "+clockID(now, 1)+" \"taken\" true\n", clockID(now, 0), "--new")
			if clockID(now, 0) != clockID(time.Now(), 0) {
				continue // the minute turned over mid-run
			}
			if got, want := strings.TrimSpace(out), clockID(now, 2); code != 0 || got != want {
				t.Errorf("code %d, got %q, want %s", code, got, want)
			}
			return
		}
		t.Skip("the minute kept turning over")
	})

	// A known minute, so the encoding itself is pinned: 2026-11-19 12:00 UTC is minute 14140080,
	// and 1414008003 in Crockford is 1a4g363.
	t.Run("encoding", func(t *testing.T) {
		at := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
		if got := clockID(at, 3); got != "1a4g363" {
			t.Errorf("got %s, want 1a4g363", got)
		}
	})
}
