// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/yottacore/shcl/source/go/v3"
)

// RefusedDir is where repair --approve-plugins moves a refused jar, under
// serverDir, so the server doesn't load it.
const RefusedDir = "plugins-refused"

// Info is what an operator sees about a jar before approving it.
type Info struct {
	Jar
	SHA256 string
	Size   int64
	Mod    time.Time
	// About is the name and version from the jar's own plugin.yml,
	// paper-plugin.yml, or Geyser's extension.yml, or empty.
	About string
}

// Describe gives the jar's hash, size, date, and what it says it is.
func Describe(j Jar) (Info, error) {
	info := Info{Jar: j}
	var sums Sums
	sum, err := sums.Sum(j.File)
	if err != nil {
		return info, err
	}
	info.SHA256 = sum
	f, err := os.OpenFile(j.File, os.O_RDONLY|syscall.O_NONBLOCK, 0) // #nosec G304 -- a jar under the configured serverDir
	if err != nil {
		return info, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return info, err
	}
	info.Size, info.Mod = fi.Size(), fi.ModTime()
	if fi.Mode().IsRegular() {
		info.About = about(f, fi.Size())
	}
	return info, nil
}

// about reads the name and version a jar claims. A jar that isn't a zip, or
// has none of the files, gives "". The jar could say anything, so this is
// only a hint for the person approving it.
func about(r io.ReaderAt, size int64) string {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return ""
	}
	for _, want := range []string{"paper-plugin.yml", "plugin.yml", "extension.yml"} {
		for _, f := range z.File {
			if f.Name != want {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			name, version := yamlNameVersion(io.LimitReader(rc, 64<<10))
			_ = rc.Close() // only read from
			if name == "" {
				return ""
			}
			return strings.TrimSpace(name + " " + version)
		}
	}
	return ""
}

// yamlNameVersion takes the top-level name and version keys, which is all
// about needs, so no YAML library.
func yamlNameVersion(r io.Reader) (name, version string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "name":
			name = value
		case "version":
			version = value
		}
	}
	return clean(name), clean(version)
}

// clean drops control characters, so a jar can't write escape codes to the
// terminal, and cuts a long value.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
}

// Approve writes the hashes of jars into the approved list, adding the ones
// it doesn't have. Other entries, comments, and sources stay as they were. A
// new file is made readable by all, so status works without sudo.
func Approve(file string, jars []Info) error {
	doc, st := shcl.LoadFile(file)
	fresh := st == shcl.FileNotFound
	switch {
	case fresh:
		doc = shcl.New()
	case st == shcl.FileUnreadable:
		return fmt.Errorf("can't read %s", file)
	}
	for _, j := range jars {
		if !insideServer(j.Name) {
			return fmt.Errorf("%q isn't a path under the server folder", j.Name)
		}
		key := "jar(" + shcl.QuoteSegment(j.Name) + ").sha256"
		if s := doc.SetString(key, j.SHA256); s != shcl.SetOk {
			return fmt.Errorf("%s: can't add %s: %s", file, j.Name, s)
		}
	}
	if err := doc.SaveFile(file); err != nil {
		return err
	}
	if fresh {
		return os.Chmod(file, 0o644) // #nosec G302 -- the list isn't secret, and status reads it without sudo
	}
	return nil
}

// Refuse moves a jar from pinnedDirs into RefusedDir, owned like plugins/.
// One that's already there with the same name gets the time added to the new
// one's name.
func Refuse(serverDir string, j Jar, now time.Time) (string, error) {
	if !slices.Contains(pinnedDirs, path.Dir(j.Name)) {
		return "", fmt.Errorf("%s isn't a plugin, so it stays where it is", j.Name)
	}
	plugins, err := os.Stat(filepath.Join(serverDir, "plugins"))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(serverDir, RefusedDir)
	switch err := os.Mkdir(dir, 0o750); {
	case err == nil:
		if st, ok := plugins.Sys().(*syscall.Stat_t); ok {
			if err := os.Lchown(dir, int(st.Uid), int(st.Gid)); err != nil {
				return "", err
			}
		}
	case !errors.Is(err, fs.ErrExist):
		return "", err
	}
	dest := filepath.Join(dir, path.Base(j.Name))
	if _, err := os.Lstat(dest); err == nil {
		dest += "." + now.Format("20060102-150405")
	}
	// A link goes as the link; Rename doesn't follow it.
	return dest, os.Rename(filepath.Join(serverDir, filepath.FromSlash(j.Name)), dest)
}
