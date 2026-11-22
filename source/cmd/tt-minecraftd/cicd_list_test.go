// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// cicd.bash --list prints the test IDs of the steps and areas the other options give, not every ID.
func TestCicdListFollowsRun(t *testing.T) {
	defer testid.Line(t, "1a4yzbg")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	script := cicdFile(t, "cicd.bash")
	repo := filepath.Dir(filepath.Dir(script))
	// Each Go test's ID, and the folder its file is in, from the repo root.
	idRe := regexp.MustCompile(`defer testid\.Line\([tf], "([0-9a-z]+)"\)`)
	goDir := map[string]string{}
	err = filepath.WalkDir(filepath.Join(repo, "source"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p) // #nosec G304 -- the repo's own test files
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, filepath.Dir(p))
		for _, m := range idRe.FindAllSubmatch(b, -1) {
			goDir[string(m[1])] = filepath.ToSlash(rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const rootCheck, internalTest, cliTest = "1a3fh00", "1a3ee18", "1a3egc8"
	list := func(args ...string) map[string]string {
		t.Helper()
		// #nosec G204 -- test runs the repo's own script
		cmd := exec.Command(bash, append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "CICD_MEM_CAP=none")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		got := map[string]string{}
		for _, m := range regexp.MustCompile(`(?m)^  ([0-9a-z]{7}) (\S+)`).FindAllStringSubmatch(string(out), -1) {
			got[m[1]] = m[2]
		}
		n := regexp.MustCompile(`(?m)^(\d+) test\(s\) with IDs:`).FindStringSubmatch(string(out))
		if n == nil {
			t.Fatalf("%v: no count line:\n%s", args, out)
		}
		return got
	}

	cli := list("--list", "--backlog-run-quick", "cli")
	if len(cli) == 0 || cli[cliTest] == "" {
		t.Errorf("cli area: %d IDs, %s missing", len(cli), cliTest)
	}
	for id := range cli {
		if goDir[id] != "source/cmd/tt-minecraftd" {
			t.Errorf("cli area lists %s %s, from %q", id, cli[id], goDir[id])
		}
	}

	whole := list("--merge-gate", "--list")
	for _, id := range []string{rootCheck, internalTest, cliTest} {
		if whole[id] == "" {
			t.Errorf("merge gate list has no %s", id)
		}
	}

	if none := list("--list", "--backlog-run-quick", "--no-test"); len(none) != 0 {
		t.Errorf("a gate with no tests lists %d IDs", len(none))
	}

	fuzzSoak := list("--merge-gate", "--list", "--no-test", "--no-race", "--no-arm64", "--no-roothost", "--no-rootzfs")
	if len(fuzzSoak) == 0 {
		t.Error("fuzz and soak list nothing")
	}
	for id, name := range fuzzSoak {
		if !strings.HasPrefix(name, "Fuzz") && name != "TestSoak" {
			t.Errorf("fuzz and soak list %s %s", id, name)
		}
	}
	if roots := list("--merge-gate", "--list", "--no-test", "--no-race", "--no-arm64", "--no-fuzz", "--no-soak"); roots[rootCheck] == "" || roots[cliTest] != "" {
		t.Errorf("root host checks alone: %d IDs, %s there: %v, %s there: %v", len(roots), rootCheck, roots[rootCheck] != "", cliTest, roots[cliTest] != "")
	}
}
