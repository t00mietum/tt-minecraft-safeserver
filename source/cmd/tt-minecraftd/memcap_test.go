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

// fMemScope from mem-cap.bash, on a stand-in systemd-run that prints its args. The scope needs
// OOMPolicy=continue: under systemd's default of stop, one step killed at the cap ends the whole
// run, which then never sees the 137 or cleans up.
func TestMemScopeOOMPolicy(t *testing.T) {
	defer testid.Line(t, "1a4yyfc")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	lib := cicdFile(t, filepath.Join("utility", "include", "mem-cap.bash"))
	dir := t.TempDir()
	for name, body := range map[string]string{
		"systemd-run": "#!/bin/sh\nprintf '%s\\n' \"$@\"\n",
		"choom":       "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil { // #nosec G306 -- a stand-in that has to run
			t.Fatal(err)
		}
	}
	script := "set -euo pipefail\nunset GITHUB_ACTIONS CICD_MEM_SCOPED\n" +
		"PATH=" + shQuote(dir) + ":\"${PATH}\"; CICD_MEM_CAP=1g; CICD_SYSTEMD_RUN=" + shQuote(filepath.Join(dir, "systemd-run")) + "\n" +
		"source " + shQuote(lib) + "\nfMemCap\nfMemScope run.bash a\necho 'no scope'\n"
	// #nosec G204 -- test runs functions from the repo's own script
	out, err := exec.Command(bash, "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	args := strings.Join(strings.Split(strings.TrimSpace(string(out)), "\n"), " ")
	for _, want := range []string{"-p MemoryMax=1G", "-p MemorySwapMax=0", "-p OOMPolicy=continue", "choom -n 1000 -- bash run.bash a"} {
		if !strings.Contains(args, want) {
			t.Errorf("systemd-run args have no %q: %s", want, args)
		}
	}
}
