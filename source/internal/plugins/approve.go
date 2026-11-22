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
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/yottacore/shcl/source/go/v3"
)

// Info is a jar with what's known about it.
type Info struct {
	Jar
	SHA256 string
	Size   int64
	Mod    time.Time
	// About is the name and version from the jar's own plugin.yml,
	// paper-plugin.yml, or Geyser's extension.yml, or empty.
	About string
	// Source goes in the list for update --plugins, when it's known.
	Source string
}

// Describe gives the jar's hash, size, date, and what it says it is, all
// from one open, so the hash is of the jar the rest describes. Anything but
// a regular file is refused before it's opened.
func Describe(j Jar) (Info, error) {
	info := Info{Jar: j}
	f, err := server.OpenRegular(j.File)
	if err != nil {
		return info, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return info, err
	}
	if info.SHA256, err = hashOf(f); err != nil {
		return info, err
	}
	info.Size, info.Mod = fi.Size(), fi.ModTime()
	info.About = about(f, fi.Size())
	return info, nil
}

// about reads the name and version a jar claims. A jar that isn't a zip, or
// has none of the files, gives "". The jar could say anything, so this is
// only a hint.
func about(r io.ReaderAt, size int64) string {
	name, version := claims(r, size)
	return strings.TrimSpace(name + " " + version)
}

// aboutFile is about for a jar on disk, with no hash, since setup and repair
// only want the name.
func aboutFile(file string) (string, error) {
	f, err := server.OpenRegular(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	return about(f, fi.Size()), nil
}

// Claims gives the plugin name and version an open jar says it is, from the
// same files as About, or "" for both.
func Claims(f *os.File) (name, version string) {
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return "", ""
	}
	return claims(f, fi.Size())
}

func claims(r io.ReaderAt, size int64) (name, version string) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return "", ""
	}
	for _, want := range []string{"paper-plugin.yml", "plugin.yml", "extension.yml"} {
		for _, f := range z.File {
			if f.Name != want {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", ""
			}
			name, version = yamlNameVersion(io.LimitReader(rc, 64<<10))
			_ = rc.Close() // only read from
			if name == "" {
				return "", ""
			}
			return name, version
		}
	}
	return "", ""
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

// clean drops control characters and the ones that flip text direction, so
// a jar can't write escape codes to the terminal or disguise its name, and
// cuts a long value on a character boundary.
func clean(s string) string {
	return server.Cut(server.Clean(s), 80)
}

// SetSources writes each jar's source into plugins.shcl, adding the jars it
// doesn't have. Other entries, comments, and the operator's own lines stay
// byte for byte. A new file is made readable by all, so status works
// without sudo.
func SetSources(file string, jars []Info) error {
	doc, st, err := shcl.LoadFileKeepLines(file, shcl.Standard)
	fresh := st == shcl.FileNotFound
	switch {
	case err != nil:
		return err
	case st == shcl.FileUnreadable:
		return fmt.Errorf("can't read %s", file)
	}
	for _, j := range jars {
		if !insideServer(j.Name) {
			return fmt.Errorf("%q isn't a path under the server folder", j.Name)
		}
		if j.Source == "" {
			continue
		}
		key := "jar(" + shcl.QuoteSegment(j.Name) + ").source"
		if s := doc.SetString(key, j.Source); s != shcl.SetOk {
			return fmt.Errorf("%s: can't add %s's source: %s", file, j.Name, s)
		}
	}
	if _, err := doc.SaveFileKeepLines(file); err != nil {
		return err
	}
	if fresh {
		return os.Chmod(file, 0o644) // #nosec G302 -- the list isn't secret, and status reads it without sudo
	}
	return nil
}

// Unsourced lists the jars in plugins/ that plugins.shcl gives no source,
// each with About, so setup and repair can give Geyser and Floodgate
// theirs. No list yet means none has one. A jar that leads out of serverDir
// and trees isn't read, since this runs as root; start refuses it.
func Unsourced(serverDir string, trees []string, file string) ([]Info, error) {
	listed, _, err := ReadApproved(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	has := make(map[string]bool, len(listed))
	for _, a := range listed {
		has[a.Name] = a.Source != ""
	}
	jars, err := Jars(serverDir)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, j := range jars {
		if path.Dir(j.Name) != "plugins" || has[j.Name] {
			continue
		}
		if _, err := Within(j.File, serverDir, trees); err != nil {
			continue
		}
		about, err := aboutFile(j.File)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", j.Name, err)
		}
		out = append(out, Info{Jar: j, About: about})
	}
	return out, nil
}
