// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package config

import (
	"fmt"
	"os"

	"github.com/yottacore/shcl/source/go/v3"
)

// Warning is a problem in one line of a file, or between 2 keys, when File is
// empty. The rest still loads.
type Warning struct {
	File string
	Line int
	Text string
}

func (w Warning) String() string {
	switch {
	case w.File == "":
		return w.Text
	case w.Line > 0:
		return fmt.Sprintf("%s:%d: %s", w.File, w.Line, w.Text)
	}
	return w.File + ": " + w.Text
}

// LoadFile parses an shcl file. Only a file that can't be read is an error.
// Parse errors come back as warnings, since shcl repairs a bad line and
// carries on. Its hints are dropped: the readers here say more about the
// same lines, and a hint such as "did you mean a list" misleads for a key
// that takes one value.
func LoadFile(path string) (*shcl.Document, []Warning, error) {
	text, err := os.ReadFile(path) // #nosec G304 -- the operator picks the path, with --config
	if err != nil {
		return nil, nil, err
	}
	doc := shcl.Parse(string(text))
	var warns []Warning
	if doc.NewerFormat() {
		warns = append(warns, Warning{path, 0, "written for a newer shcl format than this build reads"})
	}
	for _, d := range doc.Diagnostics() {
		if d.Severity == shcl.SeverityError {
			warns = append(warns, Warning{path, d.Line, d.Message})
		}
	}
	return doc, warns, nil
}
