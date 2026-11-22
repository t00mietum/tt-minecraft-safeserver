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

// Pulls the named functions out of checks.bash, so they can run alone.
func checksFuncs(t *testing.T, names ...string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "..", "cicd", "root-host", "checks.bash"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(text), "\n")
	var b strings.Builder
	for _, name := range names {
		found := false
		for i, l := range lines {
			if !strings.HasPrefix(l, name+"() {") {
				continue
			}
			found = true
			for j := i; j < len(lines); j++ {
				b.WriteString(lines[j] + "\n")
				if j == i && strings.HasSuffix(l, "}") || lines[j] == "}" {
					break
				}
			}
			break
		}
		if !found {
			t.Fatalf("no %s in checks.bash", name)
		}
	}
	return b.String()
}

// sudo runs the command as is, and nft prints NFT_OUT and exits NFT_RC.
func runChecksFunc(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stubs := map[string]string{
		"sudo": "#!@BASH@\nexec \"$@\"\n",
		"nft":  "#!@BASH@\nprintf '%s' \"${NFT_OUT:-}\"\nexit \"${NFT_RC:-0}\"\n",
	}
	for name, text := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(strings.ReplaceAll(text, "@BASH@", bashPath)), 0o755); err != nil { // #nosec G306 -- a test stub
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "run.bash")
	text := "set -uo pipefail\n" + checksFuncs(t, "say", "has", "counter", "before", "fStun") + "log=\"${RIG_LOG}\"\n\"$@\"\n"
	if err := os.WriteFile(script, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(dir, "latest.log")
	if err := os.WriteFile(logFile, []byte("[Server thread/INFO]: STUN discovery through stun.example maps this host to 192.0.2.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(bashPath, append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "RIG_LOG=" + logFile}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestRootHostNumbers(t *testing.T) {
	defer testid.Line(t, "1a4q3sg")

	packets := func(n string) string {
		return `{"nftables": [{"metainfo": {"json_schema_version": 1}}, {"counter": {"family": "inet", "name": "out_local", "table": "tt_minecraftd", "packets": ` + n + `, "bytes": 0}}]}`
	}
	cases := []struct {
		name string
		env  []string
		args []string
		ok   bool
	}{
		{"counter gives the packet count", []string{"NFT_OUT=" + packets("7")}, []string{"counter", "out_local"}, true},
		{"counter fails when nft fails", []string{"NFT_RC=1"}, []string{"counter", "out_local"}, false},
		{"counter fails on a table with no counter", []string{"NFT_OUT={\"nftables\": []}"}, []string{"counter", "out_local"}, false},
		{"STUN check passes on a zero count", []string{"NFT_OUT=" + packets("0")}, []string{"fStun"}, true},
		{"STUN check fails on a nonzero count", []string{"NFT_OUT=" + packets("3")}, []string{"fStun"}, false},
		{"STUN check fails when nft fails", []string{"NFT_RC=1"}, []string{"fStun"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := runChecksFunc(t, c.env, c.args...)
			if (code == 0) != c.ok {
				t.Errorf("exit %d, want ok=%v\n%s", code, c.ok, out)
			}
			if c.ok && c.args[0] == "counter" && strings.TrimSpace(out) != "7" {
				t.Errorf("got %q, want 7", out)
			}
		})
	}

	adopt := map[string]struct {
		text string
		ok   bool
	}{
		"sources before the question":    {"Jars:\n  source: https://a\nGo ahead? [y/N]\n", true},
		"no source lines":                {"Jars:\nGo ahead? [y/N]\n", false},
		"no question":                    {"Jars:\n  source: https://a\n", false},
		"sources after the question":     {"Go ahead? [y/N]\n  source: https://a\n", false},
		"a second question after a list": {"Go ahead? [y/N]\n  source: https://a\nGo ahead? [y/N]\n", false},
	}
	for name, c := range adopt {
		t.Run("adopt: "+name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "adopt.out")
			if err := os.WriteFile(f, []byte(c.text), 0o600); err != nil {
				t.Fatal(err)
			}
			out, code := runChecksFunc(t, nil, "before", "  source: ", "Go ahead", f)
			if (code == 0) != c.ok {
				t.Errorf("exit %d, want ok=%v\n%s", code, c.ok, out)
			}
		})
	}
}

// A console that closes the pty, then exits a moment later. The pty's EIO
// comes before the child can be reaped.
func runConsolePy(t *testing.T, exitAfter string) (string, int) {
	t.Helper()
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	fake := "#!" + bashPath + "\ntrap '' INT\nexec < /dev/null > /dev/null 2>&1\nsleep " + exitAfter + "\n"
	if err := os.WriteFile(filepath.Join(dir, "tt-minecraftd"), []byte(fake), 0o755); err != nil { // #nosec G306 -- a test stub
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "cicd", "root-host", "console.py"))
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(python, "-I", script, "say hi")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestRootHostConsoleReap(t *testing.T) {
	defer testid.Line(t, "1a4q3wm")

	t.Run("a console that exits just after closing the pty left", func(t *testing.T) {
		out, code := runConsolePy(t, "0.5")
		if code != 0 {
			t.Errorf("exit %d, want 0\n%s", code, out)
		}
	})
	// The control: one that never exits still fails.
	t.Run("a console that stays fails", func(t *testing.T) {
		out, code := runConsolePy(t, "30")
		if code == 0 || !strings.Contains(out, "didn't leave on Ctrl-C") {
			t.Errorf("exit %d, want a failure\n%s", code, out)
		}
	})
}

// sudo for the look checks: zfs list, nft list, podman exec and nsenter act
// as the env says, and a command run in the container or as the server is refused with
// WRITE_RC, except a test, which works.
const looksRigSudo = `#!@BASH@
look() { case "$1" in test) exit 0 ;; *) exit "${WRITE_RC:-1}" ;; esac; }
case "$1 $2" in
"zfs list")
	[[ -z "${ZFS_FAIL:-}" ]] || { echo "cannot open 'x/mc': dataset does not exist" >&2; exit 1; }
	printf '%s' "${ZFS_OUT:-}"; exit 0 ;;
"nft list")
	printf '%s' "${NFT_TABLES:-}"; exit "${NFT_RC:-0}" ;;
"podman exec")
	[[ -z "${NO_CONTAINER:-}" ]] || { echo 'Error: no container with name or ID "tt-minecraftd" found' >&2; exit 125; }
	shift 3; look "$@" ;;
"nsenter -t")
	[[ "$3" =~ ^[0-9]+$ ]] || { echo "nsenter: failed to parse pid: '$3'" >&2; exit 1; }
	shift 8; look "$@" ;;
esac
exit 0
`

// Functions from checks.bash run with the look stubs, in a folder whose
// paper.jar links to a root-owned file.
func runLooks(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	sh, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Skip("no /bin/sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	server := filepath.Join(dir, "server")
	for _, d := range []string{bin, server} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(sh, filepath.Join(server, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	stubs := map[string]string{
		"sudo":          looksRigSudo,
		"tt-minecraftd": "#!@BASH@\nprintf '%s' \"${TT_OUT:-}\"\n",
		"pgrep":         "#!@BASH@\n[[ -n \"${PGREP_OUT:-}\" ]] || exit 1\necho \"${PGREP_OUT}\"\n",
		"id":            "#!@BASH@\necho 1000\n",
		"systemctl":     "#!@BASH@\n[[ \"$1\" != is-enabled ]] || exit 1\nprintf '%s\\n' ProtectSystem=strict NoNewPrivileges=yes PrivateDevices=yes 'ReadOnlyPaths=/srv/plugins/Geyser-Spigot.jar'\n",
	}
	for name, text := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(strings.ReplaceAll(text, "@BASH@", bashPath)), 0o755); err != nil { // #nosec G306 -- a test stub
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "run.bash")
	text := "set -uo pipefail\n" + checksFuncs(t, "say", "has", "lacks", "refused", "fZfsOurs", "snaps", "none",
		"fJarsInContainer", "fZfsPool", "fZfsNone", "fNativeStart", "fFirewallOff") +
		"dir=\"${RIG_DIR}\" pool=ttmcss-test-x ds=ttmcss-test-x/mc\n\"$@\"\n"
	if err := os.WriteFile(script, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(bashPath, append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "RIG_DIR=" + server,
		"ZFS_FILE=" + filepath.Join(dir, "tmp", "x", "zpool.img")}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

// A check that passes on a refusal or on no match fails when the look itself
// couldn't run.
func TestRootHostLooks(t *testing.T) {
	defer testid.Line(t, "1a4z0h0")
	cases := []struct {
		name string
		env  []string
		args []string
		ok   bool
	}{
		{"jars: refused in the container", nil, []string{"fJarsInContainer"}, true},
		{"jars: no container", []string{"NO_CONTAINER=1"}, []string{"fJarsInContainer"}, false},
		{"jars: writable", []string{"WRITE_RC=0"}, []string{"fJarsInContainer"}, false},
		{"zfs none: no snapshots", nil, []string{"fZfsNone"}, true},
		{"zfs none: zfs list fails", []string{"ZFS_FAIL=1"}, []string{"fZfsNone"}, false},
		{"zfs none: a snapshot", []string{"ZFS_OUT=ttmcss-test-x/mc@s\n"}, []string{"fZfsNone"}, false},
		{"zfs pool: no snapshots", nil, []string{"fZfsPool"}, true},
		{"zfs pool: zfs list fails", []string{"ZFS_FAIL=1"}, []string{"fZfsPool"}, false},
		{"native: refused as the server", []string{"PGREP_OUT=123"}, []string{"fNativeStart"}, true},
		{"native: no server process", nil, []string{"fNativeStart"}, false},
		{"native: writable", []string{"PGREP_OUT=123", "WRITE_RC=0"}, []string{"fNativeStart"}, false},
		{"firewall off: table gone", []string{"TT_OUT=its table removed\n", "NFT_TABLES=table inet other\n"}, []string{"fFirewallOff"}, true},
		{"firewall off: nft fails", []string{"TT_OUT=its table removed\n", "NFT_RC=1"}, []string{"fFirewallOff"}, false},
		{"firewall off: table there", []string{"TT_OUT=its table removed\n", "NFT_TABLES=table inet tt_minecraftd\n"}, []string{"fFirewallOff"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := runLooks(t, c.env, c.args...)
			if (code == 0) != c.ok {
				t.Errorf("exit %d, want ok=%v\n%s", code, c.ok, out)
			}
		})
	}

	f := filepath.Join(t.TempDir(), "x.out")
	if err := os.WriteFile(f, []byte("one\nsha256: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"lacks: no match", []string{"lacks", "two", f}, true},
		{"lacks: a match", []string{"lacks", "sha256:", f}, false},
		{"lacks: a match with a flag", []string{"lacks", "-i", "SHA256:", f}, false},
		{"lacks: no file", []string{"lacks", "two", f + ".gone"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, code := runLooks(t, nil, c.args...)
			if (code == 0) != c.ok {
				t.Errorf("exit %d, want ok=%v\n%s", code, c.ok, out)
			}
		})
	}
}
