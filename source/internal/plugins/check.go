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
	"path"
	"path/filepath"
	"slices"
	"sort"
	"syscall"
	"time"
)

// Check is how the jars in plugins/ compare with the approved list.
type Check struct {
	Jars    int
	New     []string // in plugins/, not on the list
	Changed []string // on the list, with another hash
	Missing []string // on the list, not in plugins/
}

// OK says every jar there is approved.
func (c Check) OK() bool {
	return len(c.New) == 0 && len(c.Changed) == 0
}

// Sums hashes jars, and keeps each sum until the file's size or time changes,
// so watch doesn't read every jar on every redraw.
type Sums struct {
	seen map[string]seenSum
}

type seenSum struct {
	size int64
	mod  time.Time
	sum  string
}

// ErrNotRegular is a jar that's a pipe, a device, or a folder. The server
// owns plugins/, so it could leave one there to hang a reader.
var ErrNotRegular = errors.New("not a regular file")

// Sum gives the lower-case hex SHA-256 of the file, after links.
func (s *Sums) Sum(file string) (string, error) {
	// O_NONBLOCK, so a pipe doesn't hang the open.
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0) // #nosec G304 -- a jar under the configured serverDir
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s: %w", file, ErrNotRegular)
	}
	if old, ok := s.seen[file]; ok && old.size == fi.Size() && old.mod.Equal(fi.ModTime()) {
		return old.sum, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if s.seen == nil {
		s.seen = make(map[string]seenSum)
	}
	s.seen[file] = seenSum{fi.Size(), fi.ModTime(), sum}
	return sum, nil
}

// Folders whose jars start checks, besides paper.jar. Geyser extensions run
// with the same access as plugins.
var pinnedDirs = []string{"plugins", "plugins/Geyser-Spigot/extensions"}

// Jar is one jar that start checks: its name on the approved list, and its
// file.
type Jar struct {
	Name string
	File string
}

// Jars lists what start checks: the jars in pinnedDirs, and the build
// paper.jar points to, named by its path under serverDir. A missing paper.jar
// is left to start's own check.
func Jars(serverDir string) ([]Jar, error) {
	var jars []Jar
	for _, dir := range pinnedDirs {
		found, err := filepath.Glob(filepath.Join(serverDir, filepath.FromSlash(dir), "*.jar"))
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			jars = append(jars, Jar{dir + "/" + filepath.Base(f), f})
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
		return nil, fmt.Errorf("paper.jar points to %s, outside %s, so it can't be approved", paper, serverDir)
	}
	return append(jars, Jar{filepath.ToSlash(rel), paper}), nil
}

// CheckJars compares the jars from Jars with approved. Missing lists only the
// approved jars in pinnedDirs, since old Paper builds stay on the list.
func CheckJars(serverDir string, approved []Approved, sums *Sums) (Check, error) {
	var c Check
	jars, err := Jars(serverDir)
	if err != nil {
		return c, err
	}
	want := make(map[string]string, len(approved))
	for _, a := range approved {
		want[a.Name] = a.SHA256
	}
	c.Jars = len(jars)
	for _, j := range jars {
		sum, err := sums.Sum(j.File)
		if err != nil && !errors.Is(err, ErrNotRegular) {
			return c, err
		}
		approvedSum, listed := want[j.Name]
		delete(want, j.Name)
		switch {
		case !listed:
			c.New = append(c.New, j.Name)
		case approvedSum != sum:
			c.Changed = append(c.Changed, j.Name)
		}
	}
	for name := range want {
		if slices.Contains(pinnedDirs, path.Dir(name)) && path.Ext(name) == ".jar" {
			c.Missing = append(c.Missing, name)
		}
	}
	sort.Strings(c.Missing)
	return c, nil
}
