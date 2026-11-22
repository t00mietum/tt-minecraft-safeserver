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
	"unicode"
	"unicode/utf8"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/linksafe"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
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
// as plugins.shcl keeps it.
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
	case !jarName.MatchString(b.Name) || !named(b.Name, project):
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
	top, err := linksafe.Open(serverDir)
	if err != nil {
		return err
	}
	defer top.Close()
	dir, err := top.Mkdir("plugins", nil)
	if errors.Is(err, fs.ErrExist) {
		dir, err = top.Sub("plugins")
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	return a.fetch().Into(ctx, dir.Fd(), top.Name("plugins"), download.File{Name: b.Name, URL: b.URL, SHA256: b.SHA256})
}

// pluginNames are the names GeyserMC's own jars give in plugin.yml. Add-ons
// such as GeyserExtras or a Floodgate add-on start with the same word.
var pluginNames = map[string]string{"geyser": "Geyser-Spigot", "floodgate": "floodgate"}

// named tells if a file name in plugins/ is the project's: the project's
// name, then anything but a letter or digit, so GeyserExtras.jar isn't.
func named(file, p string) bool {
	rest, ok := strings.CutPrefix(strings.ToLower(file), p)
	if !ok || rest == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// Named says whether a jar in plugins/, by its path under serverDir, has a
// GeyserMC project's file name, as Project needs.
func Named(name string) bool {
	if path.Dir(name) != "plugins" {
		return false
	}
	for _, p := range Projects {
		if named(path.Base(name), p) {
			return true
		}
	}
	return false
}

// Project is the GeyserMC project a jar in plugins/ is, when its file name
// is the project's and the plugin name it claims is GeyserMC's own, or "".
// claim is the jar's About, the plugin name and then its version. setup and
// repair give such a jar GeyserMC's source when it has none, so update
// --plugins keeps it current, as it does the ones setup got.
func Project(name, claim string) string {
	if path.Dir(name) != "plugins" {
		return ""
	}
	file := path.Base(name)
	plugin, _, _ := strings.Cut(claim, " ")
	for _, p := range Projects {
		if named(file, p) && strings.EqualFold(plugin, pluginNames[p]) {
			return p
		}
	}
	return ""
}

// Installed tells which projects have a jar in serverDir's plugins folder,
// by the jar's name, as GeyserMC names them. A jar whose plugin.yml names
// another plugin, such as an add-on, doesn't count. One that names none is
// taken by its name, as setup keeps whatever jar a tree has.
func Installed(serverDir string) (map[string]bool, error) {
	dir := serverDir + "/plugins"
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	has := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		for _, p := range Projects {
			if !named(e.Name(), p) {
				continue
			}
			if claim := claimed(dir + "/" + e.Name()); claim == "" || strings.EqualFold(claim, pluginNames[p]) {
				has[p] = true
			}
		}
	}
	return has, nil
}

// claimed is the plugin name a jar says it is, or "" when it can't be read.
func claimed(file string) string {
	f, err := server.OpenRegular(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	name, _ := plugins.Claims(f)
	return name
}
