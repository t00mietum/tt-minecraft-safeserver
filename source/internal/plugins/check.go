// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
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

// CheckJars compares serverDir/plugins/*.jar with approved. Approved entries
// outside plugins/ are left to their own checks, such as paper.jar's.
func CheckJars(serverDir string, approved []Approved, sums *Sums) (Check, error) {
	var c Check
	jars, err := filepath.Glob(filepath.Join(serverDir, "plugins", "*.jar"))
	if err != nil {
		return c, err
	}
	want := make(map[string]string, len(approved))
	for _, a := range approved {
		if path.Dir(a.Name) == "plugins" && path.Ext(a.Name) == ".jar" {
			want[a.Name] = a.SHA256
		}
	}
	c.Jars = len(jars)
	for _, j := range jars {
		name := "plugins/" + filepath.Base(j)
		sum, err := sums.Sum(j)
		if err != nil && !errors.Is(err, ErrNotRegular) {
			return c, err
		}
		approvedSum, listed := want[name]
		delete(want, name)
		switch {
		case !listed:
			c.New = append(c.New, name)
		case approvedSum != sum:
			c.Changed = append(c.Changed, name)
		}
	}
	for name := range want {
		c.Missing = append(c.Missing, name)
	}
	sort.Strings(c.Missing)
	return c, nil
}
