// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
)

// Sum gives the lower-case hex SHA-256 of the file, after links. Anything
// but a regular file is refused before it's opened (server.ErrNotRegular).
func Sum(file string) (string, error) {
	f, err := server.OpenRegular(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashOf(f)
}

func hashOf(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Folders whose jars run as plugins, besides paper.jar. Geyser extensions run
// with the same access as plugins.
var pinnedDirs = []string{"plugins", "plugins/Geyser-Spigot/extensions"}

// Jar is one jar the server loads: its path under serverDir, and its file.
type Jar struct {
	Name string
	File string
}

// Jars lists what the server loads: the jars in pinnedDirs, and the build
// paper.jar points to, named by its path under serverDir. Container start
// mounts each one read-only. A missing paper.jar is left to start's own check.
func Jars(serverDir string) ([]Jar, error) {
	var jars []Jar
	for _, dir := range pinnedDirs {
		// Not filepath.Glob, whose stat and then plain open of the folder
		// leave a gap to swap in a link to a device. os.ReadDir opens with
		// O_DIRECTORY, which no device passes. Like Glob, a folder that's
		// missing or can't be read has no jars.
		folder := filepath.Join(serverDir, filepath.FromSlash(dir))
		entries, _ := os.ReadDir(folder)
		for _, e := range entries {
			if ok, _ := filepath.Match("*.jar", e.Name()); ok {
				jars = append(jars, Jar{dir + "/" + e.Name(), filepath.Join(folder, e.Name())})
			}
		}
	}
	paper, err := filepath.EvalSymlinks(filepath.Join(serverDir, "paper.jar"))
	if errors.Is(err, fs.ErrNotExist) {
		return jars, nil
	}
	if err != nil {
		return nil, err
	}
	base, err := filepath.EvalSymlinks(serverDir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, paper)
	if err != nil || !insideServer(filepath.ToSlash(rel)) {
		return nil, fmt.Errorf("paper.jar points to %s, outside %s", server.Clean(paper), serverDir)
	}
	return append(jars, Jar{filepath.ToSlash(rel), paper}), nil
}
