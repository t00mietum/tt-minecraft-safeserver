// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package oldbuild keeps a hand-installed build that's replaced or in the
// way, rather than deleting it, and GFS-rotates the ones it keeps. See
// design.md, Boot.
package oldbuild

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/linksafe"
)

const (
	// Dir is off PATH, so nothing runs a kept build by accident.
	Dir    = "/var/lib/tt-minecraftd/old-builds"
	prefix = "tt-minecraftd."
	stamp  = "20060102-150405"
)

// How many of each the rotation keeps, as cicd's gfs-rotate.bash does, plus
// the very first one.
const (
	keepFrequent = 10
	keepHourly   = 4
	keepDaily    = 5
	keepWeekly   = 4
	keepMonthly  = 4
	keepYearly   = 2
)

// Name is the file name a build with mtime gets in Dir.
func Name(mtime time.Time) string {
	return prefix + mtime.Format(stamp)
}

// Where is the path Retire would give the build at path, for a plan or a
// report. A build from the same second as a kept one gets a suffix.
func Where(path, dir string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Name(fi.ModTime())), nil
}

// Same says whether the files at a and b have the same SHA-256. One that
// can't be read isn't the same.
func Same(a, b string) bool {
	if fa, err := os.Stat(a); err == nil {
		if fb, err := os.Stat(b); err == nil && fa.Size() != fb.Size() {
			return false
		}
	}
	sa, err := sum(a)
	if err != nil {
		return false
	}
	sb, err := sum(b)
	return err == nil && bytes.Equal(sa, sb)
}

func sum(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- a build of the tool
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Retire moves the build at path into dir, named for its mtime, and then
// rotates dir. It gives where the build went. dir is made 0755 when it's
// missing. A path on another filesystem is copied, then removed.
func Retire(path, dir string, now time.Time) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s isn't a plain file, so it's left where it is", path)
	}
	if err := ready(dir); err != nil {
		return "", err
	}
	from, err := linksafe.Open(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer from.Close()
	to, err := linksafe.Open(dir)
	if err != nil {
		return "", err
	}
	defer to.Close()

	base := filepath.Base(path)
	want := Name(fi.ModTime())
	name, err := freeName(want, func(n string) error { return from.Move(base, to, n) })
	if errors.Is(err, syscall.EXDEV) {
		name, err = copyOver(path, to, want)
		if err == nil {
			err = os.Remove(path)
		}
	}
	if err != nil {
		return "", err
	}
	_, err = Rotate(dir, now)
	return filepath.Join(dir, name), err
}

// freeName tries put with want, then want.2 and so on while the name is
// taken, and gives the name that worked.
func freeName(want string, put func(name string) error) (string, error) {
	name := want
	for i := 2; ; i++ {
		err := put(name)
		if !errors.Is(err, fs.ErrExist) || i > 99 {
			return name, err
		}
		name = want + "." + strconv.Itoa(i)
	}
}

// ready makes dir when it's missing, and refuses one that's a link, or that
// someone other than root could write to while root runs this.
func ready(dir string) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- /var/lib folders are 0755, and this one has only root-written builds
			return err
		}
		// Past the umask.
		return os.Chmod(dir, 0o755) // #nosec G302 -- a folder anyone may list, as /var/lib's are
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s isn't a folder", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 && (st.Uid != 0 || fi.Mode().Perm()&0o022 != 0) {
		return fmt.Errorf("%s can be changed by an account other than root; it should be root's, mode 0755", dir)
	}
	return nil
}

// copyOver copies path into to under a free name near want, with its mode
// and mtime, by way of a temp file.
func copyOver(path string, to linksafe.Dir, want string) (string, error) {
	src, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // #nosec G304 -- the build being moved
	if err != nil {
		return "", err
	}
	defer src.Close()
	fi, err := src.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s isn't a plain file", path)
	}
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tmp := ".tt-minecraftd.tmp-" + hex.EncodeToString(b[:])
	tmpPath := to.Name(tmp)
	dst, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600) // #nosec G304 -- a new name in the old-builds folder
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath) // a half copy is no use
		}
	}()
	_, err = io.Copy(dst, src)
	if err == nil {
		err = dst.Chmod(fi.Mode().Perm())
	}
	if err == nil {
		err = dst.Sync()
	}
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chtimes(tmpPath, fi.ModTime(), fi.ModTime())
	}
	if err != nil {
		return "", err
	}
	name, err := freeName(want, func(n string) error { return to.Move(tmp, to, n) })
	keep = err == nil
	return name, err
}

type kept struct {
	name string
	at   time.Time
}

// Rotate prunes dir down to the newest build in each of the last few
// finished hours, days, ISO weeks, months, and years, plus the newest 10
// and the very first. It gives the names it removed. Only plain files named
// tt-minecraftd.* count; anything else in dir is left alone.
func Rotate(dir string, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	loc := now.Location()
	var builds []kept
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		at, err := when(dir, e, loc)
		if err != nil {
			continue
		}
		builds = append(builds, kept{e.Name(), at})
	}
	if len(builds) == 0 {
		return nil, nil
	}
	slices.SortStableFunc(builds, func(a, b kept) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})

	keep := map[int]bool{0: true, len(builds) - 1: true}
	// The period now is still open, so its newest isn't its last yet.
	for _, p := range []struct {
		key func(t time.Time) string
		n   int
	}{
		{func(t time.Time) string { return t.Format("2006") }, keepYearly},
		{func(t time.Time) string { return t.Format("200601") }, keepMonthly},
		{func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%04d%02d", y, w) }, keepWeekly},
		{func(t time.Time) string { return t.Format("20060102") }, keepDaily},
		{func(t time.Time) string { return t.Format("2006010215") }, keepHourly},
	} {
		open := p.key(now)
		last := map[string]int{}
		var keys []string
		for i, b := range builds {
			k := p.key(b.at.In(loc))
			if k == open {
				continue
			}
			if _, seen := last[k]; !seen {
				keys = append(keys, k)
			}
			last[k] = i
		}
		slices.Sort(keys)
		for _, k := range keys[max(0, len(keys)-p.n):] {
			keep[last[k]] = true
		}
	}
	// The newest few that no period claimed, as gfs-rotate.bash counts them.
	for i := max(0, len(builds)-keepFrequent); i < len(builds); i++ {
		keep[i] = true
	}

	var pruned []string
	for i, b := range builds {
		if keep[i] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, b.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return pruned, err
		}
		pruned = append(pruned, b.name)
	}
	return pruned, nil
}

// when is a build's time, from its name, or its mtime for a name that has
// none.
func when(dir string, e fs.DirEntry, loc *time.Location) (time.Time, error) {
	rest := strings.TrimPrefix(e.Name(), prefix)
	if len(rest) >= len(stamp) {
		if at, err := time.ParseInLocation(stamp, rest[:len(stamp)], loc); err == nil {
			return at, nil
		}
	}
	fi, err := os.Lstat(filepath.Join(dir, e.Name()))
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime().In(loc), nil
}
