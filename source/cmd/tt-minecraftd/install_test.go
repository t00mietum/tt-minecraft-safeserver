// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A stand-in GitHub for install.bash: curl, wget, uname, and sudo on a PATH
// that has nothing else but the tools the script needs.
type installRig struct {
	t                         *testing.T
	bin, rel, dest, log, tty  string
	old                       string
	noRetire                  bool
	osRelease, arch, bashPath string
	systemd, sys              string
	releases                  []rigRelease
}

type rigRelease struct {
	tag          string
	pre, draft   bool
	badSum, nada bool // a wrong sum listed, or no binary at all
	broken       bool // a binary that matches its sum and won't run
}

const rigCurl = `#!@BASH@
out=-; url=
while (($#)); do case "$1" in -o|-O) out="$2"; shift 2 ;; -*) shift ;; *) url="$1"; shift ;; esac; done
echo "fetch ${url}" >> "${RIG_LOG}"
case "${url}" in
	"https://api.github.com/repos/t00mietum/tt-minecraft-safeserver/releases?per_page=100") f="${RIG_REL}/releases.json" ;;
	https://github.com/t00mietum/tt-minecraft-safeserver/releases/download/*) f="${RIG_REL}/${url#https://github.com/t00mietum/tt-minecraft-safeserver/releases/download/}" ;;
	*) exit 6 ;;
esac
[[ -f "${f}" ]] || exit 22
if [[ "${out}" == - ]]; then cat "${f}"; else cat "${f}" > "${out}"; fi
`

const rigUname = `#!@BASH@
case "$1" in -s) echo Linux ;; -m) echo "${RIG_ARCH}" ;; esac
`

// The rig has no root, so the owner is logged and left out.
const rigSudo = `#!@BASH@
echo "sudo $*" >> "${RIG_LOG}"
args=()
while (($#)); do
	if [[ "$1" == -o || "$1" == -g ]] && [[ "${2:-}" == root ]]; then shift 2; continue; fi
	args+=("$1"); shift
done
exec "${args[@]}"
`

const rigExe = `#!@BASH@
case "$1" in
	--version) printf 'tt-minecraftd @TAG@ build test (2026-11-14)\nCopyright\n' ;;
	setup) shift; echo "setup $*" >> "${RIG_LOG}" ;;
	retire-build)
		echo "retire-build $2" >> "${RIG_LOG}"
		[[ -z "${RIG_NO_RETIRE:-}" ]] || exit 1
		install -d "${RIG_OLD}" && mv "$2" "${RIG_OLD}/tt-minecraftd.old" ;;
esac
`

func newInstallRig(t *testing.T, fetcher string) *installRig {
	t.Helper()
	dir := t.TempDir()
	r := &installRig{t: t, bin: filepath.Join(dir, "bin"), rel: filepath.Join(dir, "rel"), dest: filepath.Join(dir, "usr-local-bin"),
		log: filepath.Join(dir, "log"), old: filepath.Join(dir, "old-builds"), tty: filepath.Join(dir, "tty"), osRelease: filepath.Join(dir, "os-release"), arch: "x86_64",
		systemd: filepath.Join(dir, "run-systemd-system"), sys: filepath.Join(dir, "sys")}
	var err error
	if r.bashPath, err = exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	for _, d := range []string{r.bin, r.rel, r.dest, r.systemd, r.sys} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"bash", "awk", "sort", "id", "sha256sum", "mktemp", "install", "rm", "rmdir", "cat", "mv", "tr"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if err := os.Symlink(p, filepath.Join(r.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	r.script(fetcher, rigCurl)
	r.script("uname", rigUname)
	r.script("sudo", rigSudo)
	r.write(r.osRelease, "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n")
	r.write(r.tty, "\n")
	return r
}

func (r *installRig) write(path, text string) {
	r.t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *installRig) script(name, text string) {
	r.t.Helper()
	path := filepath.Join(r.bin, name)
	r.write(path, strings.ReplaceAll(text, "@BASH@", r.bashPath))
	if err := os.Chmod(path, 0o755); err != nil {
		r.t.Fatal(err)
	}
}

// publish writes the release list and each release's files, newest last
// in the list so a test can't pass on list order.
func (r *installRig) publish(rels ...rigRelease) {
	r.t.Helper()
	r.releases = rels
	var js strings.Builder
	js.WriteString("[\n")
	for i, rel := range rels {
		if i > 0 {
			js.WriteString(",\n")
		}
		fmt.Fprintf(&js, "  {\n    \"url\": \"x\",\n    \"tag_name\": \"%s\",\n    \"name\": \"%s\",\n    \"draft\": %t,\n    \"prerelease\": %t,\n    \"assets\": [\n      {\n        \"name\": \"tt-minecraftd-linux-amd64\"\n      }\n    ]\n  }", rel.tag, rel.tag, rel.draft, rel.pre)
		d := filepath.Join(r.rel, rel.tag)
		if err := os.Mkdir(d, 0o755); err != nil {
			r.t.Fatal(err)
		}
		var sums strings.Builder
		for _, arch := range []string{"amd64", "arm64"} {
			name := "tt-minecraftd-linux-" + arch
			body := strings.ReplaceAll(strings.ReplaceAll(rigExe, "@BASH@", r.bashPath), "@TAG@", rel.tag)
			if rel.broken {
				body = "#!" + r.bashPath + "\nexit 3\n"
			}
			if !rel.nada {
				r.write(filepath.Join(d, name), body)
			}
			sum := sha256.Sum256([]byte(body))
			if rel.badSum {
				sum[0] ^= 1
			}
			fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), name)
			fmt.Fprintf(&sums, "%s  %s.deb\n", strings.Repeat("0", 64), name)
		}
		r.write(filepath.Join(d, "SHA256SUMS"), sums.String())
	}
	js.WriteString("\n]\n")
	r.write(filepath.Join(r.rel, "releases.json"), js.String())
}

func (r *installRig) run(args ...string) (string, int) {
	r.t.Helper()
	_ = os.Remove(r.log)
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "install.bash"))
	if err != nil {
		r.t.Fatal(err)
	}
	// catatonit and nft are looked for in r.sys only, so the host's don't count.
	all := append([]string{"-c", `source "$1"; destDir="$2"; osRelease="$3"; ttyDev="$4"; systemdDir="$5"; catatonitPaths="$6/catatonit"; nftPaths="$6/nft"; shift 6; fMain "$@"`,
		"rig", script, r.dest, r.osRelease, r.tty, r.systemd, r.sys}, args...)
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(r.bashPath, all...)
	tmp := r.t.TempDir()
	cmd.Env = []string{"PATH=" + r.bin, "HOME=" + r.t.TempDir(), "TMPDIR=" + tmp, "RIG_LOG=" + r.log, "RIG_REL=" + r.rel, "RIG_ARCH=" + r.arch, "RIG_OLD=" + r.old}
	if r.noRetire {
		cmd.Env = append(cmd.Env, "RIG_NO_RETIRE=1")
	}
	out, err := cmd.CombinedOutput()
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		r.t.Errorf("left %s in the temp folder", left[0].Name())
	}
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return string(out), code
}

// have puts what setup would otherwise get through apt in place: podman
// and docker on PATH, catatonit and nft where the script looks, or with
// "bin/" on PATH.
func (r *installRig) have(what ...string) {
	r.t.Helper()
	for _, w := range what {
		switch {
		case w == "podman" || w == "docker":
			r.script(w, "#!/bin/sh\n")
		case strings.HasPrefix(w, "bin/"):
			r.script(strings.TrimPrefix(w, "bin/"), "#!/bin/sh\n")
		default:
			r.write(filepath.Join(r.sys, w), "x\n")
		}
	}
}

func (r *installRig) logText() string {
	b, _ := os.ReadFile(r.log)
	return string(b)
}

// installed returns the tag the installed stand-in reports, or "".
func (r *installRig) installed() string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dest, "tt-minecraftd"))
	if err != nil {
		return ""
	}
	if fi, _ := os.Stat(filepath.Join(r.dest, "tt-minecraftd")); fi.Mode().Perm() != 0o755 {
		r.t.Errorf("installed mode %v, want 0755", fi.Mode().Perm())
	}
	for _, rel := range r.releases {
		if strings.Contains(string(b), "tt-minecraftd "+rel.tag+" build") {
			return rel.tag
		}
	}
	return "?"
}

func TestInstallScript(t *testing.T) {
	defer testid.Line(t, "1a3jrx0")

	t.Run("stable over a newer pre-release, then setup with its options", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0-beta.1", pre: true}, rigRelease{tag: "v1.0.0"}, rigRelease{tag: "v0.9.0"}, rigRelease{tag: "v1.1.0", draft: true})
		out, code := r.run("--yes", "--", "--yes", "--accept-eula")
		if code != 0 || r.installed() != "v1.0.0" {
			t.Fatalf("code %d, installed %q\n%s", code, r.installed(), out)
		}
		if !strings.Contains(r.logText(), "setup --yes --accept-eula\n") {
			t.Errorf("setup didn't get its options:\n%s", r.logText())
		}
		// 2026-11-19: a writable folder skipped sudo, and the binary stayed the operator's (2026111715275548).
		if os.Geteuid() != 0 && !strings.Contains(r.logText(), "sudo install -m 0755 -o root -g root ") {
			t.Errorf("didn't install as root on a writable folder:\n%s", r.logText())
		}
		if strings.Contains(r.logText(), "retire-build") {
			t.Errorf("moved a build that wasn't there:\n%s", r.logText())
		}
	})

	t.Run("no stable release takes the newest pre-release", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0-alpha.10", pre: true}, rigRelease{tag: "v1.0.0-alpha.2", pre: true}, rigRelease{tag: "v1.0.0-alpha.9", pre: true})
		out, code := r.run("--yes")
		if code != 0 || r.installed() != "v1.0.0-alpha.10" || !strings.Contains(out, "no stable release yet") {
			t.Fatalf("code %d, installed %q\n%s", code, r.installed(), out)
		}
	})

	t.Run("dev takes a pre-release newer than the stable one", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v0.9.0"}, rigRelease{tag: "v1.0.0-rc.1", pre: true}, rigRelease{tag: "v1.0.0-beta.3", pre: true})
		out, code := r.run("--release=dev", "--yes")
		if code != 0 || r.installed() != "v1.0.0-rc.1" {
			t.Fatalf("code %d, installed %q\n%s", code, r.installed(), out)
		}
	})

	t.Run("wget when there's no curl, and the arm64 build", func(t *testing.T) {
		r := newInstallRig(t, "wget")
		r.arch = "aarch64"
		r.publish(rigRelease{tag: "v1.0.0"})
		out, code := r.run("--yes")
		if code != 0 || r.installed() != "v1.0.0" || !strings.Contains(r.logText(), "/v1.0.0/tt-minecraftd-linux-arm64\n") {
			t.Fatalf("code %d, installed %q\n%s\n%s", code, r.installed(), out, r.logText())
		}
	})

	t.Run("a current binary isn't fetched again, and setup still runs", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		if _, code := r.run("--yes"); code != 0 {
			t.Fatal("first run failed")
		}
		out, code := r.run("--yes")
		if code != 0 || strings.Contains(r.logText(), "/tt-minecraftd-linux-") || !strings.Contains(r.logText(), "setup") || strings.Contains(r.logText(), "retire-build") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
		if !strings.Contains(out, "already v1.0.0") {
			t.Errorf("plan doesn't say it's current:\n%s", out)
		}
	})

	t.Run("an older install is replaced", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		if _, code := r.run("--yes"); code != 0 || r.installed() != "v1.0.0" {
			t.Fatal("first run failed")
		}
		if err := os.RemoveAll(r.rel); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(r.rel, 0o755); err != nil {
			t.Fatal(err)
		}
		r.publish(rigRelease{tag: "v1.0.0"}, rigRelease{tag: "v1.1.0"})
		out, code := r.run("--yes")
		if code != 0 || r.installed() != "v1.1.0" || !strings.Contains(out, "replacing v1.0.0") {
			t.Fatalf("code %d, installed %q\n%s", code, r.installed(), out)
		}
	})

	t.Run("a local build with the release's version is replaced", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		dest := filepath.Join(r.dest, "tt-minecraftd")
		local := strings.ReplaceAll(strings.ReplaceAll(rigExe, "@BASH@", r.bashPath), "@TAG@", "v1.0.0") + "# dogfood\n"
		r.write(dest, local)
		if err := os.Chmod(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		out, code := r.run("--yes")
		got, _ := os.ReadFile(dest)
		want, _ := os.ReadFile(filepath.Join(r.rel, "v1.0.0", "tt-minecraftd-linux-amd64"))
		if code != 0 || string(got) != string(want) || !strings.Contains(out, "replacing a v1.0.0 build that isn't the release's") {
			t.Fatalf("code %d, kept the local build %t\n%s\n%s", code, string(got) == local, out, r.logText())
		}
		if !strings.Contains(r.logText(), "setup") {
			t.Errorf("setup didn't run:\n%s", r.logText())
		}
	})

	t.Run("a different build is moved out of the way by the new one, and kept", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		dest := filepath.Join(r.dest, "tt-minecraftd")
		local := strings.ReplaceAll(strings.ReplaceAll(rigExe, "@BASH@", r.bashPath), "@TAG@", "v0.9.0") + "# hand build\n"
		r.write(dest, local)
		if err := os.Chmod(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		out, code := r.run("--yes")
		kept, _ := os.ReadFile(filepath.Join(r.old, "tt-minecraftd.old"))
		if code != 0 || r.installed() != "v1.0.0" || string(kept) != local || !strings.Contains(out, "Old build ..: moves to /var/lib/tt-minecraftd/old-builds") {
			t.Fatalf("code %d, installed %q, kept %t\n%s\n%s", code, r.installed(), string(kept) == local, out, r.logText())
		}
		if !strings.Contains(r.logText(), "retire-build "+dest+"\n") {
			t.Errorf("the staged build didn't move the old one:\n%s", r.logText())
		}
		if left, _ := os.ReadDir(r.dest); len(left) != 1 {
			t.Errorf("left %v", left)
		}
	})

	t.Run("a failed move installs nothing and keeps the old build", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		dest := filepath.Join(r.dest, "tt-minecraftd")
		r.write(dest, "#!/bin/sh\n# hand build\n")
		r.noRetire = true
		out, code := r.run("--yes")
		got, _ := os.ReadFile(dest)
		if code == 0 || string(got) != "#!/bin/sh\n# hand build\n" || strings.Contains(r.logText(), "setup") || !strings.Contains(out, "nothing was installed") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
		if left, _ := os.ReadDir(r.dest); len(left) != 1 {
			t.Errorf("left %v", left)
		}
	})

	t.Run("SHA256SUMS without this CPU's build stops before the prompt", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		r.write(filepath.Join(r.rel, "v1.0.0", "SHA256SUMS"), strings.Repeat("0", 64)+"  tt-minecraftd-linux-arm64\n")
		out, code := r.run()
		if code == 0 || r.installed() != "" || !strings.Contains(out, "doesn't list tt-minecraftd-linux-amd64") || strings.Contains(out, "Go ahead?") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("a wrong sum installs nothing and skips setup", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0", badSum: true})
		out, code := r.run("--yes")
		if code == 0 || r.installed() != "" || strings.Contains(r.logText(), "setup") || !strings.Contains(out, "doesn't match SHA256SUMS") {
			t.Fatalf("code %d, installed %q\n%s", code, r.installed(), out)
		}
	})

	t.Run("a missing binary installs nothing", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0", nada: true})
		out, code := r.run("--yes")
		if code == 0 || r.installed() != "" || strings.Contains(r.logText(), "setup") {
			t.Fatalf("code %d, installed %q\n%s", code, r.installed(), out)
		}
	})

	t.Run("no at the prompt changes nothing, yes goes ahead", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		out, code := r.run()
		if code == 0 || r.installed() != "" || strings.Contains(r.logText(), "/tt-minecraftd-linux-") || strings.Contains(r.logText(), "setup") {
			t.Fatalf("empty answer went ahead: code %d\n%s\n%s", code, out, r.logText())
		}
		r.write(r.tty, "y\n")
		out, code = r.run()
		if code != 0 || r.installed() != "v1.0.0" {
			t.Fatalf("yes didn't go ahead: code %d\n%s", code, out)
		}
	})

	t.Run("no terminal and no --yes reads as no", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		r.tty = filepath.Join(t.TempDir(), "gone")
		if out, code := r.run(); code == 0 || r.installed() != "" || !strings.Contains(out, "Add --yes") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})

	t.Run("a build that won't run installs nothing and keeps the old one", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0", broken: true})
		dest := filepath.Join(r.dest, "tt-minecraftd")
		r.write(dest, "#!/bin/sh\n# hand build\n")
		out, code := r.run("--yes")
		got, _ := os.ReadFile(dest)
		if code == 0 || string(got) != "#!/bin/sh\n# hand build\n" || strings.Contains(r.logText(), "setup") || !strings.Contains(out, "won't run on this box (exit 3)") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
		if strings.Contains(r.logText(), "retire-build") {
			t.Errorf("moved the old build for one that won't run:\n%s", r.logText())
		}
		if left, _ := os.ReadDir(r.dest); len(left) != 1 {
			t.Errorf("left %v", left)
		}
	})

	t.Run("SHA256SUMS with upper-case hashes and CRLF", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		f := filepath.Join(r.rel, "v1.0.0", "SHA256SUMS")
		b, _ := os.ReadFile(f)
		var sums strings.Builder
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			sum, name, _ := strings.Cut(line, "  ")
			sums.WriteString(strings.ToUpper(sum) + "  " + name + "\r\n")
		}
		r.write(f, sums.String())
		if out, code := r.run("--yes"); code != 0 || r.installed() != "v1.0.0" {
			t.Fatalf("code %d\n%s", code, out)
		}
	})

	t.Run("a web page in place of the binary is named as one", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		r.write(filepath.Join(r.rel, "v1.0.0", "tt-minecraftd-linux-amd64"), "<html>Sign in to the hotel wifi</html>\n")
		out, code := r.run("--yes")
		if code == 0 || r.installed() != "" || !strings.Contains(out, "came back as a web page") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})

	t.Run("a web page in place of the release list isn't read as no releases", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		r.write(filepath.Join(r.rel, "releases.json"), "<html>Sign in to the hotel wifi</html>\n")
		out, code := r.run("--yes")
		if code == 0 || strings.Contains(out, "no stable release") || !strings.Contains(out, "isn't a release list") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})

	t.Run("a tag with a path in it is refused before it reaches a URL", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish()
		r.write(filepath.Join(r.rel, "releases.json"), "[\n  {\n    \"tag_name\": \"v9.0.0/../../../other/repo\",\n    \"draft\": false,\n    \"prerelease\": false\n  }\n]\n")
		out, code := r.run("--yes")
		if code == 0 || !strings.Contains(out, "isn't a plain tag name") || strings.Contains(r.logText(), "/releases/download/") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("option values take any case, and errors have a blank line either side", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v0.9.0"}, rigRelease{tag: "v1.0.0-rc.1", pre: true})
		if out, code := r.run("--release", "DEV", "--yes"); code != 0 || r.installed() != "v1.0.0-rc.1" {
			t.Fatalf("code %d\n%s", code, out)
		}
		for _, args := range [][]string{{"--release"}, {"--release=nightly"}, {"--bogus"}} {
			out, code := r.run(args...)
			if code != 2 || !strings.HasPrefix(out, "\n[ ERROR: ") || !strings.HasSuffix(out, " ]\n\n") || r.logText() != "" {
				t.Errorf("%v: code %d\n%q", args, code, out)
			}
		}
		r.write(r.osRelease, "ID=nixos\n")
		if out, _ := r.run("--yes"); !strings.HasPrefix(out, "\n[ ERROR: ") || !strings.HasSuffix(out, " ]\n\n") {
			t.Errorf("refusal: %q", out)
		}
	})

	t.Run("a folder the operator can't write to goes through sudo", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes anywhere")
		}
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		if err := os.Chmod(r.dest, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(r.dest, 0o755) })
		out, code := r.run("--yes")
		// The stand-in sudo has no root, so its install fails, after the plan said sudo.
		if !strings.Contains(r.logText(), "sudo install -m 0755 ") || !strings.Contains(out, ", through sudo") {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("refused before any download", func(t *testing.T) {
		for name, c := range map[string]struct{ osRelease, arch, want string }{
			"NixOS":       {"ID=nixos\n", "x86_64", "NixOS isn't supported yet"},
			"Fedora":      {"ID=fedora\nID_LIKE=\"rhel centos fedora\"\n", "x86_64", "only Debian"},
			"Fedora hint": {"ID=fedora\n", "x86_64", "--any-distro skips this check"},
			"no os file":  {"", "x86_64", "only Debian"},
			"32-bit ARM":  {"ID=debian\n", "armv7l", "no build for CPU 'armv7l'"},
			"Ubuntu":      {"ID=ubuntu\nID_LIKE=debian\n", "x86_64", ""},
			"Mint, quote": {"ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", "x86_64", ""},
		} {
			r := newInstallRig(t, "curl")
			r.publish(rigRelease{tag: "v1.0.0"})
			r.arch = c.arch
			if c.osRelease == "" {
				r.osRelease = filepath.Join(t.TempDir(), "gone")
			} else {
				r.write(r.osRelease, c.osRelease)
			}
			out, code := r.run("--yes")
			if c.want == "" {
				if code != 0 || r.installed() != "v1.0.0" {
					t.Errorf("%s: code %d\n%s", name, code, out)
				}
				continue
			}
			if code == 0 || !strings.Contains(out, c.want) || r.logText() != "" || r.installed() != "" {
				t.Errorf("%s: code %d\n%s\n%s", name, code, out, r.logText())
			}
		}
	})

	t.Run("no systemd is refused before any download", func(t *testing.T) {
		for _, args := range [][]string{{"--yes"}, {"--yes", "--any-distro"}} {
			r := newInstallRig(t, "curl")
			r.publish(rigRelease{tag: "v1.0.0"})
			r.have("docker", "nft")
			if err := os.Remove(r.systemd); err != nil {
				t.Fatal(err)
			}
			out, code := r.run(args...)
			if code == 0 || !strings.Contains(out, "needs systemd") || r.logText() != "" || r.installed() != "" {
				t.Errorf("%v: code %d\n%s\n%s", args, code, out, r.logText())
			}
		}
	})

	t.Run("--any-distro takes another distro once what setup needs is there", func(t *testing.T) {
		for name, c := range map[string]struct {
			have []string
			want string
		}{
			"Podman, catatonit, nft": {[]string{"podman", "catatonit", "nft"}, ""},
			"catatonit on PATH":      {[]string{"podman", "bin/catatonit", "nft"}, ""},
			"Docker, nft":            {[]string{"docker", "nft"}, ""},
			"Podman, no catatonit":   {[]string{"podman", "docker", "nft"}, "missing: catatonit"},
			"no runtime":             {[]string{"catatonit", "nft"}, "missing: Podman and catatonit, or Docker. "},
			"no nft":                 {[]string{"docker"}, "missing: nftables"},
			"nothing":                {nil, "missing: Podman and catatonit, or Docker, nftables"},
		} {
			r := newInstallRig(t, "curl")
			r.publish(rigRelease{tag: "v1.0.0"})
			r.write(r.osRelease, "ID=fedora\nID_LIKE=\"rhel centos fedora\"\n")
			r.have(c.have...)
			out, code := r.run("--yes", "--any-distro", "--", "--yes")
			if c.want == "" {
				if code != 0 || r.installed() != "v1.0.0" || !strings.Contains(r.logText(), "setup --yes\n") || !strings.Contains(out, "--any-distro installs nothing") {
					t.Errorf("%s: code %d\n%s\n%s", name, code, out, r.logText())
				}
				continue
			}
			if code == 0 || !strings.Contains(out, c.want) || r.logText() != "" || r.installed() != "" {
				t.Errorf("%s: code %d\n%s\n%s", name, code, out, r.logText())
			}
		}
	})

	t.Run("--any-distro still refuses NixOS", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish(rigRelease{tag: "v1.0.0"})
		r.write(r.osRelease, "ID=nixos\n")
		r.have("docker", "nft")
		if out, code := r.run("--yes", "--any-distro"); code == 0 || !strings.Contains(out, "NixOS isn't supported yet") || r.logText() != "" {
			t.Fatalf("code %d\n%s\n%s", code, out, r.logText())
		}
	})

	t.Run("no releases at all", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish()
		if out, code := r.run("--yes"); code == 0 || !strings.Contains(out, "no stable release to install yet") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})
}
