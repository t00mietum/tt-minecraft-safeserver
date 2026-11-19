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

// sudo here only logs, says yes to zpool list, and runs checks.bash's
// re-run as the operator. No real tt-minecraftd is on PATH.
const zfsRigSudo = `#!@BASH@
echo "sudo $*" >> "${RIG_LOG}"
if [[ "$1" == -u ]]; then shift 2; exec env "$@"; fi
exit 0
`

// checks.bash run by hand with ZFS_POOL set, as root-host.bash --zfs does
// in the container, which sees the host's pools.
func runZfsChecks(t *testing.T, pool string) (string, string, int) {
	t.Helper()
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	dir := t.TempDir()
	bin, logFile, home := filepath.Join(dir, "bin"), filepath.Join(dir, "log"), filepath.Join(dir, "home")
	for _, d := range []string{bin, home} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"bash", "env", "mktemp", "grep", "sed", "tail", "tee", "cat", "tr", "printf"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	stubs := map[string]string{"sudo": zfsRigSudo, "tt-minecraftd": "#!@BASH@\nexit 1\n"}
	for name, text := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(strings.ReplaceAll(text, "@BASH@", bashPath)), 0o755); err != nil { // #nosec G306 -- a test stub
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "cicd", "root-host", "checks.bash"))
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(bashPath, script)
	cmd.Dir = home
	cmd.Env = []string{"PATH=" + bin, "HOME=" + home, "TMPDIR=" + t.TempDir(), "RIG_LOG=" + logFile,
		"ZFS_POOL=" + pool, "ZFS_FILE=" + filepath.Join(dir, "tmp", "run", "zpool.img")}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	sudoLog, _ := os.ReadFile(logFile)
	return string(out), string(sudoLog), code
}

func TestRootHostZfsPoolName(t *testing.T) {
	defer testid.Line(t, "1a3jx3c")

	t.Run("a pool root-host.bash didn't name is never touched", func(t *testing.T) {
		out, sudoLog, code := runZfsChecks(t, "tank")
		if code == 0 || !strings.Contains(out, "refused: pool 'tank'") {
			t.Errorf("code %d, want a refusal\n%s", code, out)
		}
		if sudoLog != "" {
			t.Errorf("ran sudo with a pool it refused:\n%s", sudoLog)
		}
	})

	// The control: the rig reaches the destroy for a pool that is ours.
	t.Run("a ttmcss-test pool is destroyed on the way out", func(t *testing.T) {
		out, sudoLog, _ := runZfsChecks(t, "ttmcss-test-0a1b2c3d")
		if !strings.Contains(sudoLog, "sudo zpool destroy -f ttmcss-test-0a1b2c3d\n") {
			t.Errorf("no destroy of its own pool\nsudo log:\n%s\noutput:\n%s", sudoLog, out)
		}
	})
}
