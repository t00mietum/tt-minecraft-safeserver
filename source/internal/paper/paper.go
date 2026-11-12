// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package paper gets Paper builds from the PaperMC download API, checks
// them, and points paper.jar at one. setup and update --paper share it.
package paper

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
)

// DefaultBase is PaperMC's download API, called Fill.
const DefaultBase = "https://fill.papermc.io/v3/projects/paper"

// API talks to the download API. The zero value uses DefaultBase.
type API struct {
	Base      string
	Client    *http.Client
	UserAgent string
}

// Build is one downloadable Paper jar.
type Build struct {
	Version string
	ID      int
	Name    string
	SHA256  string
	Size    int64
	URL     string
}

// jarName is what the API's file name has to look like, since it becomes a
// file in the server tree.
var jarName = regexp.MustCompile(`^paper-[A-Za-z0-9._-]+\.jar$`)

func (a API) base() string {
	if a.Base == "" {
		return DefaultBase
	}
	return strings.TrimRight(a.Base, "/")
}

func (a API) fetch() download.Client {
	return download.Client{HTTP: a.Client, UserAgent: a.UserAgent}
}

// release is a version without a pre-release part, such as 26.3 or 1.21.11
// and not 26.3-rc-3.
var release = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// versionText is what can go in the API's path.
var versionText = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// NewestVersion is the newest Minecraft release Paper has a stable build for.
func (a API) NewestVersion(ctx context.Context) (string, error) {
	var project struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := a.fetch().JSON(ctx, a.base(), &project); err != nil {
		return "", err
	}
	var all []string
	for _, list := range project.Versions {
		for _, v := range list {
			if release.MatchString(v) {
				all = append(all, v)
			}
		}
	}
	sortNewest(all)
	for _, v := range all {
		if _, err := a.NewestStable(ctx, v); err == nil {
			return v, nil
		} else if !errors.Is(err, ErrNoStable) {
			return "", err
		}
	}
	return "", errors.New("the PaperMC API lists no release with a stable build")
}

// sortNewest orders releases such as 1.21.11 and 26.3 by their numbers.
func sortNewest(vs []string) {
	parts := func(v string) []int {
		var n []int
		for _, p := range strings.Split(v, ".") {
			x, _ := strconv.Atoi(p) // release has made them digits
			n = append(n, x)
		}
		return n
	}
	slices.SortFunc(vs, func(a, b string) int { return slices.Compare(parts(b), parts(a)) })
}

// ErrNoStable is a version with no stable build yet.
var ErrNoStable = errors.New("no stable build")

// NewestStable is the newest stable build of one Minecraft version.
func (a API) NewestStable(ctx context.Context, version string) (Build, error) {
	if !versionText.MatchString(version) {
		return Build{}, fmt.Errorf("%q isn't a Minecraft version", version)
	}
	var builds []struct {
		ID        int    `json:"id"`
		Channel   string `json:"channel"`
		Downloads map[string]struct {
			Name      string            `json:"name"`
			Checksums map[string]string `json:"checksums"`
			Size      int64             `json:"size"`
			URL       string            `json:"url"`
		} `json:"downloads"`
	}
	if err := a.fetch().JSON(ctx, a.base()+"/versions/"+version+"/builds", &builds); err != nil {
		return Build{}, err
	}
	best := -1
	for i, b := range builds {
		if b.Channel == "STABLE" && (best < 0 || b.ID > builds[best].ID) {
			best = i
		}
	}
	if best < 0 {
		return Build{}, fmt.Errorf("version %s: %w", version, ErrNoStable)
	}
	d, ok := builds[best].Downloads["server:default"]
	b := Build{Version: version, ID: builds[best].ID, Name: d.Name, SHA256: strings.ToLower(d.Checksums["sha256"]), Size: d.Size, URL: d.URL}
	what := fmt.Sprintf("version %s build %d", version, b.ID)
	switch {
	case !ok:
		return Build{}, errors.New(what + " has no server download")
	case !jarName.MatchString(b.Name):
		return Build{}, fmt.Errorf("%s: odd file name %q", what, b.Name)
	case !download.SHA256Hex.MatchString(b.SHA256):
		return Build{}, errors.New(what + " has no SHA-256")
	case b.Size <= 0 || b.Size > download.MaxJar:
		return Build{}, fmt.Errorf("%s: odd size %d", what, b.Size)
	case !strings.HasPrefix(b.URL, "https://"):
		return Build{}, errors.New(what + ": download isn't https")
	}
	return b, nil
}

// Install downloads b into serverDir, checks its size and SHA-256, and points
// paper.jar at it. Every file op is relative to the folder's handle, without
// following links, as the settings package does, since the server account
// may own the tree. The new files take the folder's owner and group.
func (a API) Install(ctx context.Context, serverDir string, b Build) error {
	dirfd, err := syscall.Open(serverDir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return &fs.PathError{Op: "open", Path: serverDir, Err: err}
	}
	defer syscall.Close(dirfd)
	if err := a.fetch().Into(ctx, dirfd, serverDir, download.File{Name: b.Name, URL: b.URL, SHA256: b.SHA256, Size: b.Size}); err != nil {
		return err
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(dirfd, &st); err != nil {
		return &fs.PathError{Op: "stat", Path: serverDir, Err: err}
	}
	return link(dirfd, serverDir, b.Name, st)
}

// link makes paper.jar a link to name, replacing what's there in one rename.
func link(dirfd int, serverDir, name string, st syscall.Stat_t) error {
	tmp, err := download.TempName("paper.jar")
	if err != nil {
		return err
	}
	if err := symlinkat(name, dirfd, tmp); err != nil {
		return &fs.PathError{Op: "symlink", Path: serverDir + "/" + tmp, Err: err}
	}
	if os.Geteuid() == 0 {
		if err := fchownatNoFollow(dirfd, tmp, int(st.Uid), int(st.Gid)); err != nil {
			_ = syscall.Unlinkat(dirfd, tmp) // best effort
			return &fs.PathError{Op: "chown", Path: serverDir + "/" + tmp, Err: err}
		}
	}
	if err := syscall.Renameat(dirfd, tmp, dirfd, "paper.jar"); err != nil {
		_ = syscall.Unlinkat(dirfd, tmp) // best effort
		return &fs.PathError{Op: "rename", Path: serverDir + "/paper.jar", Err: err}
	}
	return nil
}

func symlinkat(target string, dirfd int, name string) error {
	t, err := syscall.BytePtrFromString(target)
	if err != nil {
		return err
	}
	n, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_SYMLINKAT, uintptr(unsafe.Pointer(t)), uintptr(dirfd), uintptr(unsafe.Pointer(n))); errno != 0 { // #nosec G103 -- symlinkat isn't in package syscall
		return errno
	}
	return nil
}

// atSymlinkNoFollow is AT_SYMLINK_NOFOLLOW, which package syscall lacks.
const atSymlinkNoFollow = 0x100

func fchownatNoFollow(dirfd int, name string, uid, gid int) error {
	return syscall.Fchownat(dirfd, name, uid, gid, atSymlinkNoFollow)
}
