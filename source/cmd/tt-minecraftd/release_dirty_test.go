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

// fStepBuild and fReleaseDirty, copied out of cicd.bash, in a throwaway repo, with go build swapped
// for a stand-in. A release build refuses a file under source, go.mod, go.sum or config.bash that
// the commit doesn't have, untracked and ignored ones too, since go build would take it in. A dev
// build, or a file elsewhere, goes on.
func TestReleaseRefusesDirtyTree(t *testing.T) {
	defer testid.Line(t, "1a4yz24")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	src, err := os.ReadFile(cicdFile(t, "cicd.bash"))
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
	// One line; missing on the tree before the fix, where the dirty cases then build.
	dirtyFn := regexp.MustCompile(`(?m)^fReleaseDirty\(\)\{.*\n`).Find(src)

	type file struct{ path, body string }
	cases := []struct {
		name  string
		rel   string
		files []file
		want  string // a path the refusal names, or "" for a build
	}{
		{"clean", "1", nil, ""},
		{"untracked go file", "1", []file{{"source/extra.go", "package x\n"}}, "source/extra.go"},
		{"ignored go file", "1", []file{{"source/gen/zz.go", "package gen\n"}}, "source/gen/"},
		{"go.mod changed", "1", []file{{"go.mod", "module x\n\ngo 1.25\n"}}, "go.mod"},
		{"config.bash changed", "1", []file{{"cicd/config.bash", "GO_LDFLAGS_COMMON='-s'\n"}}, "cicd/config.bash"},
		{"file elsewhere", "1", []file{{"notes.txt", "x\n"}, {"README.md", "changed\n"}}, ""},
		{"dev build", "0", []file{{"source/extra.go", "package x\n"}}, ""},
	}
	for _, c := range cases {
		root := t.TempDir()
		for _, f := range []file{
			{"source/x.go", "package x\n"}, {"go.mod", "module x\n"}, {"go.sum", ""}, {"cicd/config.bash", "GO_LDFLAGS_COMMON=''\n"},
			{"README.md", "x\n"}, {".gitignore", "source/gen/\n"},
		} {
			writeRel(t, root, f.path, f.body)
		}
		git := func(args ...string) {
			t.Helper()
			// #nosec G204 -- fixed git commands on a throwaway repo
			cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		git("init", "-q")
		git("add", ".")
		git("commit", "-q", "-m", "x")
		for _, f := range c.files {
			writeRel(t, root, f.path, f.body)
		}
		script := "set -euo pipefail\ncd " + shQuote(root) + "\n" +
			"EXE_NAME=x; RELEASE_TARGETS=(linux/amd64); root=" + shQuote(root) + "; RELEASE_DIR=release; goVersion=v9.9.9; mode=quick\n" +
			"runTmp=" + shQuote(t.TempDir()) + "; GO_BUILD_FLAGS=(); GO_LDFLAGS_COMMON=''; BUILD_JOBS=1; GO_MAIN_PKG=./x\n" +
			"fEcho_Clean(){ :; }; fDie(){ echo \"die: $*\"; exit 1; }; fReleaseSums(){ :; }\n" +
			"fCommitEpoch(){ echo 0; }; fIsRelease(){ echo " + c.rel + "; }\n" +
			"fGo(){ local o=''; while (($#)); do [[ \"$1\" != -o ]] || o=\"$2\"; shift; done; echo built; printf '#!/bin/sh\\n' > \"${o}\"; chmod +x \"${o}\"; }\n" +
			string(dirtyFn) + string(build) + "fStepBuild\n"
		// #nosec G204 -- test runs functions from the repo's own script
		out, err := exec.Command(bash, "-c", script).CombinedOutput()
		got := string(out)
		if c.want == "" {
			if err != nil || !strings.Contains(got, "built") {
				t.Errorf("%s: want a build, got %v:\n%s", c.name, err, got)
			}
			continue
		}
		if err == nil || strings.Contains(got, "built") || !strings.Contains(got, "die: ") || !strings.Contains(got, c.want) {
			t.Errorf("%s: want a refusal naming %s, got %v:\n%s", c.name, c.want, err, got)
		}
	}
}

func writeRel(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
