// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package plugins

import (
	"archive/zip"
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
)

// cacheDirs are where Paper keeps code it wrote itself, and loads it again
// without checking: plugin libraries from Maven, and remapped plugins before
// 26.1.
var cacheDirs = []string{"libraries", "plugins/.paper-remapped"}

// Libraries lists the jars in cacheDirs, less the ones paper.jar puts back
// from its own hashes at every start. A link to a folder, or a link at a
// cache folder itself, is listed too, since Paper would follow it.
func Libraries(serverDir string) ([]Jar, error) {
	own, err := paperOwn(serverDir)
	if err != nil {
		return nil, err
	}
	var jars []Jar
	for _, dir := range cacheDirs {
		root := filepath.Join(serverDir, filepath.FromSlash(dir))
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == root && errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			rel, err := filepath.Rel(serverDir, p)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(rel)
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				if fi, err := os.Stat(p); p == root || err == nil && fi.IsDir() {
					jars = append(jars, Jar{name, p})
					return nil
				}
			case d.IsDir():
				return nil
			}
			if strings.HasSuffix(d.Name(), ".jar") && !own[name] {
				jars = append(jars, Jar{name, p})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return jars, nil
}

// paperOwn names the files Paperclip keeps in libraries/ for the build
// paper.jar points to, from its META-INF/libraries.list. Paperclip checks
// each against the hash there before Paper runs, and writes it again when it
// differs. A paper.jar that's missing or isn't a zip has none.
func paperOwn(serverDir string) (map[string]bool, error) {
	own := map[string]bool{}
	f, err := server.OpenRegular(filepath.Join(serverDir, "paper.jar"))
	if errors.Is(err, fs.ErrNotExist) {
		return own, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	z, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return own, nil
	}
	for _, zf := range z.File {
		if zf.Name != "META-INF/libraries.list" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return own, nil
		}
		sc := bufio.NewScanner(io.LimitReader(rc, 1<<20))
		for sc.Scan() {
			// hash, Maven id, path under libraries/
			parts := strings.Split(sc.Text(), "\t")
			if len(parts) == 3 && insideServer(parts[2]) {
				own["libraries/"+parts[2]] = true
			}
		}
		_ = rc.Close() // only read from
		break
	}
	return own, nil
}
