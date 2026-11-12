// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package paper gets Paper builds from the PaperMC download API, checks
// them, and points paper.jar at one. setup and update --paper share it.
package paper

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
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

// maxJSON caps an API answer. The build list for one version is well under it.
const maxJSON = 8 << 20

// maxJar caps a download, whatever size the API gives.
const maxJar = 512 << 20

// jarName is what the API's file name has to look like, since it becomes a
// file in the server tree.
var jarName = regexp.MustCompile(`^paper-[A-Za-z0-9._-]+\.jar$`)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (a API) base() string {
	if a.Base == "" {
		return DefaultBase
	}
	return strings.TrimRight(a.Base, "/")
}

func (a API) client() *http.Client {
	if a.Client == nil {
		return &http.Client{Timeout: 10 * time.Minute}
	}
	return a.Client
}

func (a API) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	ua := a.UserAgent
	if ua == "" {
		ua = "tt-minecraftd (https://github.com/t00mietum/tt-minecraft-safeserver)"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := a.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close() // the status is the error that matters
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

func (a API) getJSON(ctx context.Context, url string, v any) error {
	resp, err := a.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxJSON+1))
	if err != nil {
		return err
	}
	if len(b) > maxJSON {
		return fmt.Errorf("%s: answer over %d MiB", url, maxJSON>>20)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	return nil
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
	if err := a.getJSON(ctx, a.base(), &project); err != nil {
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
	if err := a.getJSON(ctx, a.base()+"/versions/"+version+"/builds", &builds); err != nil {
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
	case !sha256Hex.MatchString(b.SHA256):
		return Build{}, errors.New(what + " has no SHA-256")
	case b.Size <= 0 || b.Size > maxJar:
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
	var st syscall.Stat_t
	if err := syscall.Fstat(dirfd, &st); err != nil {
		return &fs.PathError{Op: "stat", Path: serverDir, Err: err}
	}

	resp, err := a.get(ctx, b.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp, err := tempName(b.Name)
	if err != nil {
		return err
	}
	fd, err := syscall.Openat(dirfd, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return &fs.PathError{Op: "create", Path: serverDir + "/" + tmp, Err: err}
	}
	f := os.NewFile(uintptr(fd), serverDir+"/"+tmp)
	err = fill(f, fd, resp.Body, b, st)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = syscall.Renameat(dirfd, tmp, dirfd, b.Name)
	}
	if err != nil {
		_ = syscall.Unlinkat(dirfd, tmp) // best effort; err is what matters
		return err
	}
	return link(dirfd, serverDir, b.Name, st)
}

func fill(f *os.File, fd int, body io.Reader, b Build, st syscall.Stat_t) error {
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(body, b.Size+1))
	switch {
	case err != nil:
		return fmt.Errorf("downloading %s: %w", b.Name, err)
	case n != b.Size:
		return fmt.Errorf("%s: got %d bytes, the API said %d", b.Name, n, b.Size)
	case hex.EncodeToString(sum.Sum(nil)) != b.SHA256:
		return fmt.Errorf("%s: SHA-256 doesn't match the API's; not used", b.Name)
	}
	if os.Geteuid() == 0 {
		if err := syscall.Fchown(fd, int(st.Uid), int(st.Gid)); err != nil {
			return fmt.Errorf("chown %s: %w", b.Name, err)
		}
	}
	if err := syscall.Fchmod(fd, 0o660); err != nil {
		return fmt.Errorf("chmod %s: %w", b.Name, err)
	}
	return f.Sync()
}

// link makes paper.jar a link to name, replacing what's there in one rename.
func link(dirfd int, serverDir, name string, st syscall.Stat_t) error {
	tmp, err := tempName("paper.jar")
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

func tempName(name string) (string, error) {
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	return "." + name + ".tt-minecraftd-" + hex.EncodeToString(rnd[:]), nil
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
