// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package geyser gets the Geyser and Floodgate plugins from GeyserMC's
// download API, and puts them in plugins/. setup and update --plugins share
// it. See Bedrock players in design.md.
package geyser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
)

// DefaultBase is GeyserMC's download API.
const DefaultBase = "https://download.geysermc.org/v2/projects"

// Projects are the 2 plugins Bedrock players need, by their API names.
var Projects = []string{"geyser", "floodgate"}

// Title is a project's name for messages.
func Title(project string) string {
	if project == "" {
		return ""
	}
	return strings.ToUpper(project[:1]) + project[1:]
}

// API talks to the download API. The zero value uses DefaultBase.
type API struct {
	Base      string
	Client    *http.Client
	UserAgent string
}

// Build is one plugin jar, as the API gives it for Paper.
type Build struct {
	Project string
	Version string
	ID      int
	Name    string
	SHA256  string
	URL     string
}

// Path is where the jar goes, under serverDir.
func (b Build) Path() string { return "plugins/" + b.Name }

var versionText = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var jarName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.jar$`)

func (a API) base() string {
	if a.Base == "" {
		return DefaultBase
	}
	return strings.TrimRight(a.Base, "/")
}

func (a API) fetch() download.Client {
	return download.Client{HTTP: a.Client, UserAgent: a.UserAgent}
}

// Source is the address update --plugins gets a project's newest jar from,
// as the approved list keeps it.
func (a API) Source(project string) string {
	return a.base() + "/" + project + "/versions/latest/builds/latest/downloads/spigot"
}

// Latest is a project's newest build. The download address names the version
// and build, so what's fetched is the build whose hash was given.
func (a API) Latest(ctx context.Context, project string) (Build, error) {
	var answer struct {
		Version   string `json:"version"`
		Build     int    `json:"build"`
		Downloads map[string]struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"downloads"`
	}
	if err := a.fetch().JSON(ctx, a.base()+"/"+project+"/versions/latest/builds/latest", &answer); err != nil {
		return Build{}, err
	}
	d, ok := answer.Downloads["spigot"]
	b := Build{Project: project, Version: answer.Version, ID: answer.Build, Name: d.Name, SHA256: strings.ToLower(d.SHA256)}
	what := fmt.Sprintf("%s %s build %d", Title(project), b.Version, b.ID)
	switch {
	case !versionText.MatchString(b.Version) || b.ID <= 0:
		return Build{}, fmt.Errorf("%s: odd version %q or build %d", Title(project), b.Version, b.ID)
	case !ok:
		return Build{}, errors.New(what + " has no Paper download")
	case !jarName.MatchString(b.Name) || !strings.HasPrefix(strings.ToLower(b.Name), project):
		// repair and update tell the plugin by its name.
		return Build{}, fmt.Errorf("%s: odd file name %q", what, b.Name)
	case !download.SHA256Hex.MatchString(b.SHA256):
		return Build{}, errors.New(what + " has no SHA-256")
	}
	b.URL = a.base() + "/" + project + "/versions/" + b.Version + "/builds/" + strconv.Itoa(b.ID) + "/downloads/spigot"
	return b, nil
}

// Install downloads b into serverDir's plugins folder, making the folder when
// it's missing, owned like serverDir.
func (a API) Install(ctx context.Context, serverDir string, b Build) error {
	top, err := syscall.Open(serverDir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return &fs.PathError{Op: "open", Path: serverDir, Err: err}
	}
	defer syscall.Close(top)
	dir := serverDir + "/plugins"
	err = syscall.Mkdirat(top, "plugins", 0o770)
	switch {
	case err == nil:
		if err := chownLike(top, "plugins"); err != nil {
			return &fs.PathError{Op: "chown", Path: dir, Err: err}
		}
	case !errors.Is(err, syscall.EEXIST):
		return &fs.PathError{Op: "mkdir", Path: dir, Err: err}
	}
	fd, err := syscall.Openat(top, "plugins", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return &fs.PathError{Op: "open", Path: dir, Err: err}
	}
	defer syscall.Close(fd)
	return a.fetch().Into(ctx, fd, dir, download.File{Name: b.Name, URL: b.URL, SHA256: b.SHA256})
}

// chownLike gives a new folder the owner and group of the one it's in, and
// the tree's mode past the umask. Only root can chown, and a test run isn't.
func chownLike(top int, name string) error {
	if os.Geteuid() == 0 {
		var st syscall.Stat_t
		if err := syscall.Fstat(top, &st); err != nil {
			return err
		}
		if err := syscall.Fchownat(top, name, int(st.Uid), int(st.Gid), atSymlinkNoFollow); err != nil {
			return err
		}
	}
	return syscall.Fchmodat(top, name, 0o770, 0)
}

// atSymlinkNoFollow is AT_SYMLINK_NOFOLLOW, which package syscall lacks.
const atSymlinkNoFollow = 0x100

// Project is the GeyserMC project a jar in plugins/ is, when both its file
// name and the plugin name it claims start with the project's name, or "".
// setup and repair give such a jar GeyserMC's source when they first approve
// it, so update --plugins keeps it current, as it does the ones setup got.
func Project(name, claim string) string {
	if path.Dir(name) != "plugins" {
		return ""
	}
	file, claim := strings.ToLower(path.Base(name)), strings.ToLower(claim)
	for _, p := range Projects {
		if strings.HasPrefix(file, p) && strings.HasPrefix(claim, p) {
			return p
		}
	}
	return ""
}

// Installed tells which projects have a jar in serverDir's plugins folder,
// by the jar's name, as GeyserMC names them.
func Installed(serverDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(serverDir + "/plugins")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	has := map[string]bool{}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".jar") {
			continue
		}
		for _, p := range Projects {
			if strings.HasPrefix(name, p) {
				has[p] = true
			}
		}
	}
	return has, nil
}
