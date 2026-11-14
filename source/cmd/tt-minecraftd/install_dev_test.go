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

// install-dev.bash on a PATH with stand-in uname, sudo, apt-get, git, go,
// and docker, plus only the real tools the script needs.
type devRig struct {
	t                          *testing.T
	bin, home, log, tty, ca    string
	osRelease, bashPath, aptGo string
}

const devGit = `#!@BASH@
echo "git $*" >> "${RIG_LOG}"
[[ "$1" == clone ]] || exit 1
dir="${@: -1}"
mkdir -p "${dir}/.git" && printf 'module github.com/t00mietum/tt-minecraft-safeserver\n\ngo 1.24\n' > "${dir}/go.mod"
`

const devGo = `#!@BASH@
[[ "$1" == version ]] && echo "go version go@VER@ linux/amd64"
`

// Puts each package's stand-in command on PATH, as the real install would.
const devApt = `#!@BASH@
echo "apt-get $*" >> "${RIG_LOG}"
[[ "$1" == install ]] || exit 0
for p in "$@"; do
	case "${p}" in
		git)       cp "${RIG_BIN}/.git-standin" "${RIG_BIN}/git" ;;
		golang-go) sed "s/@VER@/${RIG_APT_GO}/" "${RIG_BIN}/.go-standin" > "${RIG_BIN}/go"; chmod 755 "${RIG_BIN}/go" ;;
		docker.io) printf '#!/bin/sh\n' > "${RIG_BIN}/docker"; chmod 755 "${RIG_BIN}/docker" ;;
	esac
done
`

func newDevRig(t *testing.T) *devRig {
	t.Helper()
	dir := t.TempDir()
	r := &devRig{t: t, bin: filepath.Join(dir, "bin"), home: filepath.Join(dir, "home"), log: filepath.Join(dir, "log"),
		tty: filepath.Join(dir, "tty"), ca: filepath.Join(dir, "ca.crt"), osRelease: filepath.Join(dir, "os-release"), aptGo: "1.24.4"}
	var err error
	if r.bashPath, err = exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	for _, d := range []string{r.bin, r.home} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// sed, cp, chmod, mkdir, and sh are for the stand-ins, cat for --help.
	for _, tool := range []string{"bash", "awk", "id", "ls", "env", "sed", "cp", "chmod", "mkdir", "sh", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if err := os.Symlink(p, filepath.Join(r.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	r.script("uname", rigUname)
	r.script("sudo", rigSudo)
	r.script("apt-get", devApt)
	r.script(".git-standin", devGit)
	r.script(".go-standin", devGo)
	r.write(r.osRelease, "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n")
	r.write(r.tty, "\n")
	return r
}

func (r *devRig) write(path, text string) {
	r.t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *devRig) script(name, text string) {
	r.t.Helper()
	path := filepath.Join(r.bin, name)
	r.write(path, strings.ReplaceAll(text, "@BASH@", r.bashPath))
	if err := os.Chmod(path, 0o755); err != nil {
		r.t.Fatal(err)
	}
}

// have puts everything the script looks for in place, with Go at goVer.
func (r *devRig) have(goVer string) {
	r.t.Helper()
	r.script("git", devGit)
	r.script("go", strings.ReplaceAll(devGo, "@VER@", goVer))
	r.script("docker", "#!/bin/sh\n")
	r.write(r.ca, "x\n")
}

func (r *devRig) run(args ...string) (string, int) {
	r.t.Helper()
	_ = os.Remove(r.log)
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "install-dev.bash"))
	if err != nil {
		r.t.Fatal(err)
	}
	all := append([]string{"-c", `source "$1"; osRelease="$2"; ttyDev="$3"; caFile="$4"; shift 4; fMain "$@"`, "rig", script, r.osRelease, r.tty, r.ca}, args...)
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(r.bashPath, all...)
	cmd.Dir = r.home
	cmd.Env = []string{"PATH=" + r.bin, "HOME=" + r.home, "USER=tester", "RIG_LOG=" + r.log, "RIG_BIN=" + r.bin, "RIG_ARCH=x86_64", "RIG_APT_GO=" + r.aptGo}
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return string(out), code
}

func (r *devRig) logText() string {
	b, _ := os.ReadFile(r.log)
	return string(b)
}

func (r *devRig) cloned(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func TestInstallDevScript(t *testing.T) {
	defer testid.Line(t, "df799")

	t.Run("a bare box gets every package, then the clone", func(t *testing.T) {
		r := newDevRig(t)
		out, code := r.run("--yes")
		dir := filepath.Join(r.home, "tt-minecraft-safeserver")
		lg := r.logText()
		if code != 0 || !r.cloned(dir) {
			t.Fatalf("code %d\n%s\n%s", code, out, lg)
		}
		for _, want := range []string{
			"sudo apt-get update\n",
			"sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends git ca-certificates golang-go docker.io\n",
			"git clone --branch main -- https://github.com/t00mietum/tt-minecraft-safeserver.git " + dir + "\n",
		} {
			if !strings.Contains(lg, want) {
				t.Errorf("log lacks %q:\n%s", want, lg)
			}
		}
		if strings.Index(lg, "apt-get install") > strings.Index(lg, "git clone") {
			t.Errorf("cloned before git was installed:\n%s", lg)
		}
		if !strings.Contains(out, "cicd/cicd.bash --gate") || !strings.Contains(out, "usermod -aG docker tester") {
			t.Errorf("next steps missing:\n%s", out)
		}
	})

	t.Run("nothing missing runs no apt, and a re-run leaves the clone alone", func(t *testing.T) {
		r := newDevRig(t)
		r.have("1.24.4")
		out, code := r.run("--yes", "--dir=src/tt")
		dir := filepath.Join(r.home, "src", "tt")
		if code != 0 || !r.cloned(dir) || strings.Contains(r.logText(), "apt-get") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
		if strings.Contains(out, "usermod") {
			t.Errorf("docker group note without installing docker:\n%s", out)
		}
		out, code = r.run("--yes", "--dir", dir+"/")
		if code != 0 || r.logText() != "" || !strings.Contains(out, "already a clone") {
			t.Fatalf("re-run: code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("a fork's clone counts, an empty folder is used, anything else is refused", func(t *testing.T) {
		r := newDevRig(t)
		r.have("1.24.4")
		fork := filepath.Join(r.home, "fork")
		if err := os.MkdirAll(filepath.Join(fork, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		r.write(filepath.Join(fork, "go.mod"), "module github.com/t00mietum/tt-minecraft-safeserver\n")
		if out, code := r.run("--yes", "--dir", fork); code != 0 || r.logText() != "" {
			t.Errorf("fork: code %d\n%s\n%s", code, out, r.logText())
		}
		empty := filepath.Join(r.home, "empty")
		if err := os.Mkdir(empty, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, code := r.run("--yes", "--dir", empty); code != 0 || !r.cloned(empty) {
			t.Errorf("empty: code %d\n%s", code, out)
		}
		other := filepath.Join(r.home, "other")
		if err := os.MkdirAll(filepath.Join(other, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		r.write(filepath.Join(other, "go.mod"), "module example.com/other\n")
		// docker gone, so the refusal has to come before apt.
		_ = os.Remove(filepath.Join(r.bin, "docker"))
		out, code := r.run("--yes", "--dir", other)
		if code == 0 || r.logText() != "" || !strings.Contains(out, "isn't a clone") {
			t.Errorf("other: code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("--no-docker leaves docker out", func(t *testing.T) {
		r := newDevRig(t)
		r.have("1.24.4")
		_ = os.Remove(filepath.Join(r.bin, "docker"))
		out, code := r.run("--yes", "--no-docker")
		if code != 0 || strings.Contains(r.logText(), "apt-get") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("old Go gets golang-go, and still too old fails at the end", func(t *testing.T) {
		r := newDevRig(t)
		r.have("1.19.8")
		r.aptGo = "1.19.8"
		out, code := r.run("--yes")
		lg := r.logText()
		if code == 0 || !strings.Contains(lg, "--no-install-recommends golang-go\n") || !strings.Contains(lg, "git clone") || !strings.Contains(out, "this has Go 1.19") {
			t.Fatalf("code %d\n%s\n%s", code, out, lg)
		}
		r.aptGo = "1.22.0"
		r.have("1.19.8")
		if out, code := r.run("--yes"); code != 0 || !strings.Contains(out, "already a clone") {
			t.Fatalf("newer Go from apt: code %d\n%s", code, out)
		}
	})

	t.Run("no at the prompt changes nothing, yes goes ahead", func(t *testing.T) {
		r := newDevRig(t)
		out, code := r.run()
		if code == 0 || r.logText() != "" || !strings.Contains(out, "Nothing was changed") {
			t.Fatalf("empty answer went ahead: code %d\n%s\n%s", code, out, r.logText())
		}
		r.tty = filepath.Join(t.TempDir(), "gone")
		if out, code := r.run(); code == 0 || r.logText() != "" {
			t.Fatalf("no terminal went ahead: code %d\n%s", code, out)
		}
		r.tty = filepath.Join(t.TempDir(), "tty")
		r.write(r.tty, "y\n")
		if out, code := r.run(); code != 0 || !r.cloned(filepath.Join(r.home, "tt-minecraft-safeserver")) {
			t.Fatalf("yes didn't go ahead: code %d\n%s", code, out)
		}
	})

	t.Run("missing packages and no sudo is refused before anything runs", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root needs no sudo")
		}
		r := newDevRig(t)
		_ = os.Remove(filepath.Join(r.bin, "sudo"))
		out, code := r.run("--yes")
		if code == 0 || r.logText() != "" || !strings.Contains(out, "there's no sudo") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("refused before any change", func(t *testing.T) {
		for name, c := range map[string]struct{ osRelease, want string }{
			"NixOS":       {"ID=nixos\n", "NixOS isn't supported yet"},
			"Fedora":      {"ID=fedora\nID_LIKE=\"rhel centos fedora\"\n", "only Debian"},
			"no os file":  {"", "only Debian"},
			"Ubuntu":      {"ID=ubuntu\nID_LIKE=debian\n", ""},
			"Mint, quote": {"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", ""},
		} {
			r := newDevRig(t)
			if c.osRelease == "" {
				r.osRelease = filepath.Join(t.TempDir(), "gone")
			} else {
				r.write(r.osRelease, c.osRelease)
			}
			out, code := r.run("--yes")
			if c.want == "" {
				if code != 0 {
					t.Errorf("%s: code %d\n%s", name, code, out)
				}
				continue
			}
			if code == 0 || !strings.Contains(out, c.want) || r.logText() != "" {
				t.Errorf("%s: code %d\n%s\n%s", name, code, out, r.logText())
			}
		}
	})

	t.Run("options", func(t *testing.T) {
		r := newDevRig(t)
		if out, code := r.run("--bogus"); code != 2 || r.logText() != "" {
			t.Errorf("unknown option: code %d\n%s", code, out)
		}
		if out, code := r.run("--version"); code != 0 || !strings.Contains(out, "dev installer v") {
			t.Errorf("--version: code %d\n%s", code, out)
		}
		if out, code := r.run("--help"); code != 0 || !strings.Contains(out, "--no-docker") {
			t.Errorf("--help: code %d\n%s", code, out)
		}
	})
}
