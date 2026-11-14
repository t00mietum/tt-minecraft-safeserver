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
	osRelease, arch, bashPath string
	releases                  []rigRelease
}

type rigRelease struct {
	tag          string
	pre, draft   bool
	badSum, nada bool // a wrong sum listed, or no binary at all
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

const rigSudo = `#!@BASH@
echo "sudo $*" >> "${RIG_LOG}"
exec "$@"
`

const rigExe = `#!@BASH@
case "$1" in
	--version) printf 'tt-minecraftd @TAG@ build test (2026-11-14)\nCopyright\n' ;;
	setup) shift; echo "setup $*" >> "${RIG_LOG}" ;;
esac
`

func newInstallRig(t *testing.T, fetcher string) *installRig {
	t.Helper()
	dir := t.TempDir()
	r := &installRig{t: t, bin: filepath.Join(dir, "bin"), rel: filepath.Join(dir, "rel"), dest: filepath.Join(dir, "usr-local-bin"),
		log: filepath.Join(dir, "log"), tty: filepath.Join(dir, "tty"), osRelease: filepath.Join(dir, "os-release"), arch: "x86_64"}
	var err error
	if r.bashPath, err = exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	for _, d := range []string{r.bin, r.rel, r.dest} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"bash", "awk", "sort", "id", "sha256sum", "mktemp", "install", "rm", "rmdir", "cat"} {
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
	all := append([]string{"-c", `source "$1"; destDir="$2"; osRelease="$3"; ttyDev="$4"; shift 4; fMain "$@"`, "rig", script, r.dest, r.osRelease, r.tty}, args...)
	// #nosec G204 -- test runs the repo's own script
	cmd := exec.Command(r.bashPath, all...)
	tmp := r.t.TempDir()
	cmd.Env = []string{"PATH=" + r.bin, "HOME=" + r.t.TempDir(), "TMPDIR=" + tmp, "RIG_LOG=" + r.log, "RIG_REL=" + r.rel, "RIG_ARCH=" + r.arch}
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
	defer testid.Line(t, "df798")

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
		if strings.Contains(r.logText(), "sudo") {
			t.Errorf("used sudo on a writable folder:\n%s", r.logText())
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
		if code != 0 || strings.Contains(r.logText(), "releases/download") || !strings.Contains(r.logText(), "setup") {
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
		if code == 0 || r.installed() != "" || strings.Contains(r.logText(), "releases/download") || strings.Contains(r.logText(), "setup") {
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
		if out, code := r.run(); code == 0 || r.installed() != "" {
			t.Fatalf("code %d\n%s", code, out)
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

	t.Run("no releases at all", func(t *testing.T) {
		r := newInstallRig(t, "curl")
		r.publish()
		if out, code := r.run("--yes"); code == 0 || !strings.Contains(out, "no stable release to install yet") {
			t.Fatalf("code %d\n%s", code, out)
		}
	})
}
