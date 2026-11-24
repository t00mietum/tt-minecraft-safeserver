// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// fStepBuild, copied out of cicd.bash, as a quick run calls it, with go build swapped for a stand-in
// that writes a new binary each time. An older run's SHA256SUMS and package sit in the release
// folder first. The sums left after it have to match the new binaries and list nothing else, and
// the package step's own sums then cover every file.
func TestReleaseSums(t *testing.T) {
	defer testid.Line(t, "1a3jz50")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("no sha256sum")
	}
	src, err := os.ReadFile(cicdFile(t, "cicd.bash"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(cicdFile(t, "config.bash"))
	if err != nil {
		t.Fatal(err)
	}
	fnRe := func(name string) *regexp.Regexp {
		return regexp.MustCompile(`(?ms)^` + name + `\(\)\{[^\n]*\n.*?^\}\n`)
	}
	build := fnRe("fStepBuild").Find(src)
	if build == nil {
		t.Fatal("no fStepBuild in cicd.bash")
	}
	// Missing on the tree before the fix; the build case then fails on its sums rather than here.
	sums := fnRe("fReleaseSums").Find(src)
	pkgSums := regexp.MustCompile(`(?m)^\tmapfile -t files < <\(find "\$\{relDir\}".*\n\tfReleaseSums .*\n`).Find(fnRe("fStepPackage").Find(src))
	targets := regexp.MustCompile(`(?m)^RELEASE_TARGETS=\([^)]*\)`).Find(cfg)
	exe := regexp.MustCompile(`(?m)^EXE_NAME="[^"]*"`).Find(cfg)
	if targets == nil || exe == nil {
		t.Fatal("no RELEASE_TARGETS or EXE_NAME in config.bash")
	}

	root := t.TempDir()
	rel := filepath.Join(root, "release", "v9.9.9")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(body string) string {
		t.Helper()
		script := "set -euo pipefail\n" +
			string(exe) + "\n" + string(targets) + "\n" +
			"root=" + shQuote(root) + "; RELEASE_DIR=release; goVersion=v9.9.9; mode=quick; relDir=\"${root}/release/v9.9.9\"\n" +
			"runTmp=\"${root}/run\"; mkdir -p -- \"${runTmp}\"\n" +
			"GO_BUILD_FLAGS=(-trimpath); GO_LDFLAGS_COMMON=''; BUILD_JOBS=1; GO_MAIN_PKG=./x\n" +
			"fEcho_Clean(){ :; }; fDie(){ echo \"die: $*\" >&2; exit 1; }\n" +
			"fCommitEpoch(){ echo 0; }; fIsRelease(){ echo 0; }\n" +
			// go build's stand-in: a new binary at -o, different on every call.
			"fGo(){ local o=''; while (($#)); do [[ \"$1\" != -o ]] || o=\"$2\"; shift; done\n" +
			"\tprintf '#!/bin/sh\\necho new %s\\n' \"${o##*/}$RANDOM$RANDOM\" > \"${o}\"; chmod +x \"${o}\"; }\n" +
			string(build) + string(sums) + body + "\n"
		// #nosec G204 -- test runs functions from the repo's own script
		out, err := exec.Command(bash, "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return string(out)
	}
	check := func(what string, want []string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(rel, "SHA256SUMS"))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		var got []string
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.Fields(l); len(f) == 2 {
				got = append(got, f[1])
			}
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: SHA256SUMS lists %v, want %v", what, got, want)
		}
		cmd := exec.Command("sha256sum", "--quiet", "-c", "SHA256SUMS")
		cmd.Dir = rel
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: SHA256SUMS doesn't match the folder: %v\n%s", what, err, out)
		}
	}

	bins := []string{"tt-minecraftd-linux-amd64", "tt-minecraftd-linux-arm64"}
	pkg := "tt-minecraftd_9.9.9_amd64.deb"
	for _, f := range append([]string{pkg}, bins...) {
		if err := os.WriteFile(filepath.Join(rel, f), []byte("older run "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sha256sum", append([]string{pkg}, bins...)...)
	cmd.Dir = rel
	b, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	old := string(b)
	if err := os.WriteFile(filepath.Join(rel, "SHA256SUMS"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	run("fStepBuild")
	check("after a quick run's build", bins)
	if b, _ := os.ReadFile(filepath.Join(rel, "SHA256SUMS")); string(b) == old {
		t.Error("SHA256SUMS is still the older run's")
	}
	// The package step's own lines, over every file in the folder.
	if pkgSums == nil {
		t.Fatal("no fReleaseSums call in fStepPackage")
	}
	run(string(pkgSums))
	check("after the package step", append(append([]string{}, bins...), pkg))
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
