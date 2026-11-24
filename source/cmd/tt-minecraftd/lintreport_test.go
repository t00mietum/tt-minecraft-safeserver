// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// lint-report.bash --check, as cicd.bash --lint-check runs it, on stand-in run logs. A failed or
// aborted run is FAILED with exit 1, and a log with no closing line is UNFINISHED and left unmarked,
// so the next look sees how it ended.
func TestLintReportRunResult(t *testing.T) {
	defer testid.Line(t, "1a4yynm")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	script := cicdFile(t, filepath.Join("utility", "lint-report.bash"))
	const (
		step = "•••\n[ 20261121-100000.00: Step 3 of 4; Starting Tests ]\nok  \tx\t0.1s\n"
		done = "\n[ Summary ]\n  PASS  Tests\n\n[ tt-minecraftd CI/CD: done. ]\n\n"
	)
	cases := []struct {
		name, log, tag string
		rc             int
		marked         bool
		lines          []string
	}{
		{"clean", step + "[ 20261121-100001.00: Step 3 of 4; PASS Tests ]\n" + done, "CLEAN", 0, true, nil},
		{"warning", step + "x.go:3:1: unused\n[ 20261121-100001.00: Step 3 of 4; PASS Tests ]\n" + done, "NEW", 0, true, []string{"x.go:3:1: unused"}},
		{"died", step + "[ FAILED: go test failed ]\n[ 20261121-100001.00: Step 3 of 4; FAIL Tests ]\nCleanup: nothing to remove\n", "FAILED", 1, true,
			[]string{"[ FAILED: go test failed ]", "; FAIL Tests ]"}},
		{"aborted", step + "\n[ CICD ABORTED (exit 1) at line 9: false ]\n[ 20261121-100001.00: Step 3 of 4; FAIL Tests ]\n", "FAILED", 1, true,
			[]string{"[ CICD ABORTED (exit 1)"}},
		{"test failed", step + "--- FAIL: TestX (0.00s)\nFAIL\n[ 20261121-100001.00: Step 3 of 4; FAIL Tests ]\n", "FAILED", 1, true,
			[]string{"--- FAIL: TestX", "; FAIL Tests ]"}},
		{"unfinished", step, "UNFINISHED", 0, false, nil},
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "run_20261121-100000.log"), []byte(c.log), 0o600); err != nil {
			t.Fatal(err)
		}
		// #nosec G204 -- test runs the repo's own script
		out, err := exec.Command(bash, script, "--check", "--dir", dir).CombinedOutput()
		rc := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			rc = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		got := string(out)
		if rc != c.rc || !strings.HasPrefix(got, c.tag+" ") {
			t.Errorf("%s: got exit %d, want %s and exit %d:\n%s", c.name, rc, c.tag, c.rc, got)
		}
		for _, l := range c.lines {
			if !strings.Contains(got, l) {
				t.Errorf("%s: no %q in:\n%s", c.name, l, got)
			}
		}
		_, err = os.Stat(filepath.Join(dir, ".lint-seen"))
		if (err == nil) != c.marked {
			t.Errorf("%s: marked seen is %v, want %v", c.name, err == nil, c.marked)
		}
	}
}
