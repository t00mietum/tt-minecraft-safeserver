// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// The run log's tee and its EXIT trap, copied out of cicd.bash. Ctrl-C reaches the whole process
// group, the tee included. The trap's cleanup must still run and reach the log.
func TestRunLogCtrlC(t *testing.T) {
	defer testid.Line(t, "1a4q4vx")
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	src, err := os.ReadFile(cicdFile(t, "cicd.bash"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?m)^\s*exec > >\(.*tee .*\n(?:.*\n)*?\s*trap '[^\n]*' EXIT\n`).Find(src)
	if block == nil {
		t.Fatal("no run log tee and EXIT trap in cicd.bash")
	}
	dir := t.TempDir()
	script := "set -euo pipefail\n" +
		"root='" + dir + "'; LINT_LOG_DIR=logs; stamp=x; mkdir -p \"${root}/logs\"\n" +
		"fRunTmpDone(){ sleep 0.3; echo cleanup; echo done > \"${root}/cleaned\"; }\n" +
		"trap 'exit 130' INT; trap 'exit 143' TERM\n" +
		string(block) +
		"echo started; : > \"${root}/ready\"\n" +
		"sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "run.bash"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	// A shell that started this test in the background leaves SIGINT ignored, and children keep that.
	args := []string{bash, filepath.Join(dir, "run.bash")}
	if exec.Command("env", "--default-signal=INT", "true").Run() == nil {
		args = append([]string{"env", "--default-signal=INT"}, args...)
	}
	// #nosec G204 -- test runs lines from the repo's own script
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for i := 0; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		if i > 100 {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			t.Fatal("the script never got going")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal("the script didn't end after Ctrl-C")
	}
	if _, err := os.Stat(filepath.Join(dir, "cleaned")); err != nil {
		t.Errorf("the EXIT trap's cleanup didn't run: %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "logs", "run_x.log"))
	if !strings.Contains(string(log), "started") || !strings.Contains(string(log), "cleanup") {
		t.Errorf("run log: %q; want the start and cleanup lines", log)
	}
}
