// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxLinkHops is the kernel's own limit on links in one path.
const maxLinkHops = 40

// boxLinks finds links in the trees that work on the host but won't in the
// container, which has only what's mounted, each at its real path. Such as
// world -> /srv/worlds/current/world with current a link too: Paper would
// find no world there and make a new one (backlog 2026111409374443).
func boxLinks(trees, mounts []string) error {
	var bad []string
	for _, root := range trees {
		// A folder that can't be read is skipped; tree.ToServer and the
		// server itself say more about it than this could.
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.Type()&fs.ModeSymlink == 0 {
				return nil
			}
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil // dangles on the host too, so it's not the container's doing
			}
			if !inBox(path, mounts) {
				bad = append(bad, path+" needs to point at "+real)
			}
			return nil
		})
	}
	if len(bad) == 0 {
		return nil
	}
	if len(bad) > 10 {
		bad = append(bad[:10], fmt.Sprintf("and %d more", len(bad)-10))
	}
	return fmt.Errorf("the container mounts each tree at its real path, so these links won't work in it: %s", strings.Join(bad, "; "))
}

// inBox resolves path the way the kernel would inside the container. There,
// only the mounts and the folders above them exist.
func inBox(path string, mounts []string) bool {
	done := "/"
	rest := strings.Split(path, "/")
	for hops := 0; len(rest) > 0; {
		part := rest[0]
		rest = rest[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			done = filepath.Dir(done)
			continue
		}
		next := filepath.Join(done, part)
		if !under(next, mounts) {
			if !above(next, mounts) {
				return false
			}
			done = next
			continue
		}
		fi, err := os.Lstat(next)
		if err != nil {
			return false
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			done = next
			continue
		}
		if hops++; hops > maxLinkHops {
			return false
		}
		target, err := os.Readlink(next)
		if err != nil {
			return false
		}
		if filepath.IsAbs(target) {
			done = "/"
		}
		rest = append(strings.Split(target, "/"), rest...)
	}
	return true
}

func under(path string, mounts []string) bool {
	for _, m := range mounts {
		if path == m || strings.HasPrefix(path, m+"/") {
			return true
		}
	}
	return false
}

// above is a folder the runtime makes on the way to a mount.
func above(path string, mounts []string) bool {
	for _, m := range mounts {
		if strings.HasPrefix(m, path+"/") {
			return true
		}
	}
	return false
}
