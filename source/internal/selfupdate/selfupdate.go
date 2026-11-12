// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package selfupdate gets the newest tt-minecraftd release from GitHub,
// checks it against the release's SHA256SUMS, and swaps it in for the
// running binary in one rename. update --self uses it.
package selfupdate

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
)

// DefaultBase is the repo in GitHub's API.
const DefaultBase = "https://api.github.com/repos/t00mietum/tt-minecraft-safeserver"

// SumsName is the release's checksum file, in sha256sum's format.
const SumsName = "SHA256SUMS"

// atSymlinkNoFollow is AT_SYMLINK_NOFOLLOW, which package syscall lacks.
const atSymlinkNoFollow = 0x100

// maxBinary caps a download, whatever size the API gives.
const maxBinary = 256 << 20

// AssetName is the release file for a CPU arch, such as amd64.
func AssetName(arch string) string { return "tt-minecraftd-linux-" + arch }

// API talks to GitHub's releases API. The zero value uses DefaultBase and
// the running arch.
type API struct {
	Base      string
	Client    *http.Client
	UserAgent string
	Arch      string
}

// Release is the binary for this arch in one release.
type Release struct {
	Tag    string
	Asset  string
	URL    string
	Size   int64
	SHA256 string
}

func (a API) base() string {
	if a.Base == "" {
		return DefaultBase
	}
	return strings.TrimRight(a.Base, "/")
}

func (a API) arch() string {
	if a.Arch == "" {
		return runtime.GOARCH
	}
	return a.Arch
}

func (a API) fetch() download.Client {
	return download.Client{HTTP: a.Client, UserAgent: a.UserAgent}
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// Newest is the newest release past current, with its SHA-256 from the
// release's SHA256SUMS. found is false when current is the newest. A
// pre-release is only taken when current is one too, as the first releases
// are.
func (a API) Newest(ctx context.Context, current string) (r Release, found bool, err error) {
	cur, ok := parseVersion(current)
	if !ok {
		return Release{}, false, fmt.Errorf("this build's version, %q, isn't one a release can be compared to", current)
	}
	var list []ghRelease
	// The list, not /releases/latest, which leaves out pre-releases.
	if err := a.fetch().JSON(ctx, a.base()+"/releases?per_page=50", &list); err != nil {
		return Release{}, false, err
	}
	var best *ghRelease
	bestV := cur
	for i := range list {
		gr := &list[i]
		v, ok := parseVersion(gr.Tag)
		if !ok || gr.Draft || ((gr.Prerelease || len(v.pre) > 0) && len(cur.pre) == 0) {
			continue
		}
		if v.compare(bestV) > 0 {
			best, bestV = gr, v
		}
	}
	if best == nil {
		return Release{}, false, nil
	}
	name := AssetName(a.arch())
	var bin, sums *ghAsset
	for i := range best.Assets {
		switch best.Assets[i].Name {
		case name:
			bin = &best.Assets[i]
		case SumsName:
			sums = &best.Assets[i]
		}
	}
	switch {
	case bin == nil:
		return Release{}, false, fmt.Errorf("release %s has no %s", best.Tag, name)
	case sums == nil:
		return Release{}, false, fmt.Errorf("release %s has no %s, so %s can't be checked", best.Tag, SumsName, name)
	}
	sum, err := a.sumFor(ctx, sums.URL, name)
	if err != nil {
		return Release{}, false, fmt.Errorf("release %s: %w", best.Tag, err)
	}
	return Release{Tag: best.Tag, Asset: name, URL: bin.URL, Size: bin.Size, SHA256: sum}, true, nil
}

// sumFor reads name's line from a SHA256SUMS file.
func (a API) sumFor(ctx context.Context, url, name string) (string, error) {
	resp, err := a.fetch().Get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		sum, file, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		// sha256sum marks binary mode with a *.
		file = strings.TrimPrefix(strings.TrimLeft(file, " "), "*")
		sum = strings.ToLower(sum)
		if ok && file == name && download.SHA256Hex.MatchString(sum) {
			return sum, nil
		}
	}
	return "", fmt.Errorf("%s has no SHA-256 for %s", SumsName, name)
}

// Install downloads r beside exe, checks its size and SHA-256, and that it
// says it's r's version, then renames it over exe. It takes exe's mode,
// owner, and group. Every op is relative to the folder's handle, without
// following links.
func (a API) Install(ctx context.Context, exe string, r Release) error {
	dir, name := filepath.Dir(exe), filepath.Base(exe)
	dirfd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return &fs.PathError{Op: "open", Path: dir, Err: err}
	}
	defer syscall.Close(dirfd)
	old, err := statAt(dirfd, name)
	if err != nil {
		return &fs.PathError{Op: "stat", Path: exe, Err: err}
	}
	if old.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return fmt.Errorf("%s isn't a plain file", exe)
	}
	limit := int64(maxBinary)
	if r.Size > 0 {
		limit = r.Size + 1
	}
	s, err := a.fetch().Stage(ctx, dirfd, dir, name, r.URL, limit)
	if err != nil {
		return err
	}
	if err := ready(dirfd, dir, s, r, old); err != nil {
		s.Drop()
		return err
	}
	return s.Keep()
}

// statAt stats name in the folder open as dirfd, without following a link.
func statAt(dirfd int, name string) (syscall.Stat_t, error) {
	var st syscall.Stat_t
	fd, err := syscall.Openat(dirfd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return st, err
	}
	defer syscall.Close(fd)
	err = syscall.Fstat(fd, &st)
	return st, err
}

func ready(dirfd int, dir string, s *download.Staged, r Release, old syscall.Stat_t) error {
	switch {
	case r.Size > 0 && s.Size != r.Size:
		return fmt.Errorf("%s: got %d bytes, the release said %d", r.Asset, s.Size, r.Size)
	case s.SHA256 != r.SHA256:
		return fmt.Errorf("%s: SHA-256 doesn't match the release's %s; not used", r.Asset, SumsName)
	}
	if os.Geteuid() == 0 {
		if err := syscall.Fchownat(dirfd, s.Temp, int(old.Uid), int(old.Gid), atSymlinkNoFollow); err != nil {
			return fmt.Errorf("chown %s: %w", s.Temp, err)
		}
	}
	// No setuid or setgid, even if the old one had them.
	if err := syscall.Fchmodat(dirfd, s.Temp, old.Mode&0o777|0o100, 0); err != nil {
		return fmt.Errorf("chmod %s: %w", s.Temp, err)
	}
	return checkVersion(filepath.Join(dir, s.Temp), r.Tag)
}

// checkVersion runs the new binary's --version, which catches one built for
// another arch before it replaces this one.
func checkVersion(file, tag string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, file, "--version").Output() // #nosec G204 -- the download, once its SHA-256 matched
	if err != nil {
		return fmt.Errorf("the new build doesn't run here: %w", err)
	}
	if want := "tt-minecraftd " + tag + " "; !strings.HasPrefix(string(out), want) {
		return fmt.Errorf("the new build says it's %q, not %s", strings.TrimSpace(string(out)), tag)
	}
	return nil
}

// FromNix is whether exe is managed by Nix, so it's changed there and not
// in place: on NixOS, or run from the Nix store.
func FromNix(exe string) bool {
	if strings.HasPrefix(exe, "/nix/store/") {
		return true
	}
	_, err := os.Stat("/etc/NIXOS")
	return err == nil
}

// version is a semver tag, such as v1.0.0-alpha.1.
type version struct {
	core [3]int
	pre  []string
}

func parseVersion(s string) (version, bool) {
	var v version
	s, ok := strings.CutPrefix(s, "v")
	if !ok {
		return v, false
	}
	s, _, _ = strings.Cut(s, "+")
	s, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, false
		}
		v.core[i] = n
	}
	if hasPre {
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" {
				return v, false
			}
		}
	}
	return v, true
}

// compare orders versions the way semver does: a pre-release comes before
// its release, and numeric parts sort as numbers, before words.
func (v version) compare(o version) int {
	for i := range v.core {
		if v.core[i] != o.core[i] {
			return cmp.Compare(v.core[i], o.core[i])
		}
	}
	switch {
	case len(v.pre) == 0 && len(o.pre) == 0:
		return 0
	case len(v.pre) == 0:
		return 1
	case len(o.pre) == 0:
		return -1
	}
	for i := 0; i < len(v.pre) && i < len(o.pre); i++ {
		a, b := v.pre[i], o.pre[i]
		na, errA := strconv.Atoi(a)
		nb, errB := strconv.Atoi(b)
		switch {
		case errA == nil && errB == nil:
			if na != nb {
				return cmp.Compare(na, nb)
			}
		case errA == nil:
			return -1
		case errB == nil:
			return 1
		case a != b:
			return strings.Compare(a, b)
		}
	}
	return cmp.Compare(len(v.pre), len(o.pre))
}
