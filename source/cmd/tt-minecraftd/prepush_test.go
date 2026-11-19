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

// The repo's own cicd files. The arm64 box gets only source and project, so it skips.
func cicdFile(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "cicd", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no %s here: %v", name, err)
	}
	return p
}

// A throwaway repo holding the real hook and docs list, with stand-in gates that log their name.
type hookRig struct {
	t                    *testing.T
	dir, log, base, bash string
}

func newHookRig(t *testing.T) *hookRig {
	t.Helper()
	r := &hookRig{t: t, dir: t.TempDir()}
	var err error
	if r.bash, err = exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	r.log = filepath.Join(r.dir, "gates.log")
	for _, f := range []string{"hooks/pre-push", "docs-only-paths"} {
		b, err := os.ReadFile(cicdFile(t, f))
		if err != nil {
			t.Fatal(err)
		}
		r.write("cicd/"+f, string(b))
	}
	if err := os.Chmod(filepath.Join(r.dir, "cicd/hooks/pre-push"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write("gate.sh", "#!/bin/sh\necho \"$1\" >> \""+r.log+"\"\nexit \"${GATE_RC:-0}\"\n")
	if err := os.Chmod(filepath.Join(r.dir, "gate.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(".gitignore", "gate.sh\ngates.log\ncicd/artifacts/\n")
	r.write("README.md", "# x\n")
	r.write("project/design.md", "| Key | Default\n")
	r.write("source/main.go", "package main\n")
	r.git("init", "-q", "-b", "main")
	r.base = r.commit()
	return r
}

func (r *hookRig) write(name, text string) {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *hookRig) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *hookRig) commit() string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "x")
	return r.git("rev-parse", "HEAD")
}

// push runs the hook as git would for a push of sha over the remote's base, and gives the gates
// that ran, in order, and the hook's exit code.
func (r *hookRig) push(ref, sha, gateRC string) (string, int) {
	r.t.Helper()
	_ = os.Remove(r.log)
	// #nosec G204 -- test runs the repo's own hook
	cmd := exec.Command(r.bash, filepath.Join(r.dir, "cicd/hooks/pre-push"), "origin", "git@example.com:x.git")
	cmd.Stdin = strings.NewReader(ref + " " + sha + " " + ref + " " + r.base + "\n")
	cmd.Env = append(os.Environ(), "CICD_MERGE_GATE_CMD="+r.dir+"/gate.sh merge", "CICD_DOCS_GATE_CMD="+r.dir+"/gate.sh docs", "GATE_RC="+gateRC)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	ran, _ := os.ReadFile(r.log)
	r.t.Logf("hook: %s", out)
	return strings.Join(strings.Fields(string(ran)), " "), code
}

// A docs-only push to main still runs the quick gate, since markdownlint and the config tests
// read the docs, design.md included. Code goes through the merge gate, unless it already passed.
func TestPrePushGates(t *testing.T) {
	defer testid.Line(t, "df7ah00")
	r := newHookRig(t)
	cases := []struct {
		name, file, ref, rc, wantRan string
		wantCode                     int
	}{
		{"readme", "README.md", "refs/heads/main", "0", "docs", 0},
		{"design table", "project/design.md", "refs/heads/main", "0", "docs", 0},
		{"lint config", ".markdownlint.jsonc", "refs/heads/main", "0", "docs", 0},
		{"docs gate fails", "README.md", "refs/heads/main", "1", "docs", 1},
		{"code", "source/main.go", "refs/heads/main", "0", "merge", 0},
		{"code, gate fails", "source/main.go", "refs/heads/main", "1", "merge", 1},
		{"dev", "source/main.go", "refs/heads/dev", "1", "", 0},
	}
	for i, c := range cases {
		r.git("checkout", "-q", "--detach", r.base)
		r.write(c.file, strings.Repeat("y", i+1)+"\n")
		sha := r.commit()
		ran, code := r.push(c.ref, sha, c.rc)
		if ran != c.wantRan || code != c.wantCode {
			t.Errorf("%s: ran %q, exit %d; want %q, exit %d", c.name, ran, code, c.wantRan, c.wantCode)
		}
	}
	// The commit a whole merge gate passed on skips both gates, docs or not.
	r.git("checkout", "-q", "--detach", r.base)
	r.write("source/main.go", "package main // passed\n")
	sha := r.commit()
	r.write("cicd/artifacts/merge-gate-passed", sha+"\n")
	if ran, code := r.push("refs/heads/main", sha, "1"); ran != "" || code != 0 {
		t.Errorf("passed commit: ran %q, exit %d; want nothing, exit 0", ran, code)
	}
}

// fGateWhole, copied out of cicd.bash, over every merge step: any SKIP but arm64's, a step
// turned off, a dirty tree, or HEAD moving keeps the marker from being written.
func TestGateMarker(t *testing.T) {
	defer testid.Line(t, "df7aj00")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	src, err := os.ReadFile(cicdFile(t, "cicd.bash"))
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?ms)^fGateWhole\(\)\{\n.*?^\}\n`).Find(src)
	if fn == nil {
		t.Fatal("no fGateWhole in cicd.bash")
	}
	all := regexp.MustCompile(`(?m)^allSteps=\(([^)]*)\)`).FindSubmatch(src)
	merge := regexp.MustCompile(`(?m)^\s*\[merge\]="([^"]*)"`).FindSubmatch(src)
	if all == nil || merge == nil {
		t.Fatal("no allSteps or [merge] steps in cicd.bash")
	}
	steps := strings.Fields(string(merge[1]))
	verdict := func(setup string) string {
		t.Helper()
		script := "set -euo pipefail\n" +
			"allSteps=(" + string(all[1]) + "); stepList=(" + strings.Join(steps, " ") + ")\n" +
			"declare -A stepOff=() stepResult=()\n" +
			"for s in \"${allSteps[@]}\"; do [[ \" ${stepList[*]} \" == *\" ${s} \"* ]] || stepOff[${s}]=\"not part of the merge run\"; done\n" +
			"for s in \"${stepList[@]}\"; do stepResult[${s}]=PASS; done\n" +
			"startHead=abc; startDirty=''; nowHead=abc; nowStatus=''\n" +
			"git(){ case \"$1\" in rev-parse) echo \"${nowHead}\" ;; status) printf '%s' \"${nowStatus}\" ;; esac; }\n" +
			string(fn) + setup + "\n" +
			"if why=\"$(fGateWhole)\"; then echo write; else echo \"keep: ${why}\"; fi\n"
		// #nosec G204 -- test runs a function from the repo's own script
		out, err := exec.Command(bash, "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", setup, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := verdict(""); got != "write" {
		t.Errorf("every step PASS: %s", got)
	}
	for _, s := range steps {
		got := verdict("stepResult[" + s + "]='SKIP (away)'")
		if want := s == "arm64"; (got == "write") != want {
			t.Errorf("%s SKIP: %s", s, got)
		}
		if got := verdict("unset 'stepResult[" + s + "]'"); got == "write" {
			t.Errorf("%s with no result: %s", s, got)
		}
		if got := verdict("stepOff[" + s + "]='skipped by --no-" + s + "'"); got == "write" {
			t.Errorf("%s off: %s", s, got)
		}
	}
	for _, setup := range []string{"startDirty=' M x'", "startHead=''", "nowHead=def", "nowStatus=' M x'"} {
		if got := verdict(setup); got == "write" {
			t.Errorf("%s: %s", setup, got)
		}
	}
}
