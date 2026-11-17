// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"errors"
	"io/fs"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/linksafe"
)

// dir is an open folder in the server tree, read and written without
// following links, since repair runs as root.
type dir = linksafe.Dir

func openDir(path string) (dir, error) {
	return linksafe.Open(path)
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
