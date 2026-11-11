// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package plugins holds the approved jar list. See Plugin pinning in design.md.
package plugins

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/yottacore/shcl/source/go/v3"
)

// Approved is one jar from plugins.shcl.
type Approved struct {
	Name   string // path under serverDir, such as plugins/Geyser-Spigot.jar
	SHA256 string // lower-case hex
	Source string // where update --plugins gets it, or empty
	Line   int
}

// ReadApproved reads plugins.shcl. A bad entry gets a warning and is left
// out, so its jar counts as not approved. Only a file that can't be read is
// an error.
func ReadApproved(file string) ([]Approved, []config.Warning, error) {
	doc, warns, err := config.LoadFile(file)
	if err != nil {
		return nil, nil, err
	}
	warn := func(line int, format string, args ...any) {
		warns = append(warns, config.Warning{File: file, Line: line, Text: fmt.Sprintf(format, args...)})
	}

	for _, f := range doc.ReadChildFields("").Value {
		if f.Name != "jar" {
			warn(f.Line, "unknown key %q", f.Name)
		}
	}

	// shcl merges entries with the same path into one, so a jar listed twice
	// with 2 hashes shows up below as one with more than one sha256.
	count := doc.Count("jar")
	jars := make([]Approved, 0, count)
	for i := 0; i < count; i++ {
		if jar, ok := readJar(doc, fmt.Sprintf("jar(%d)", i), warn); ok {
			jars = append(jars, jar)
		}
	}
	return jars, warns, nil
}

func readJar(doc *shcl.Document, base string, warn func(int, string, ...any)) (Approved, bool) {
	jar := Approved{Line: doc.Line(base)}
	name := doc.ReadString(base)
	if name.Status != shcl.Good || doc.Kind(base) != shcl.KindScalar {
		warn(jar.Line, "jar needs a path, such as plugins/Geyser-Spigot.jar; left out")
		return jar, false
	}
	jar.Name = name.Value
	if !insideServer(jar.Name) {
		warn(jar.Line, "%q isn't a path under the server folder; left out", jar.Name)
		return jar, false
	}

	for _, child := range doc.ReadChildFields(base).Value {
		if child.Name != "sha256" && child.Name != "source" {
			warn(child.Line, "unknown key %q under jar %s", child.Name, jar.Name)
		}
	}

	sum := doc.ReadString(base + ".sha256")
	switch {
	case sum.Status == shcl.NotFound:
		warn(jar.Line, "%s has no sha256; left out", jar.Name)
		return jar, false
	case sum.Status == shcl.Multiple:
		warn(jar.Line, "%s has more than one sha256; left out", jar.Name)
		return jar, false
	case sum.Status != shcl.Good || !isSHA256(sum.Value):
		warn(sum.Line, "%s: sha256 isn't 64 hex digits; left out", jar.Name)
		return jar, false
	}
	jar.SHA256 = strings.ToLower(sum.Value)

	// A bad source only costs update --plugins; the hash still approves the jar.
	src := doc.ReadString(base + ".source")
	switch src.Status {
	case shcl.NotFound:
	case shcl.Good:
		if u, err := url.Parse(src.Value); err == nil && u.Scheme == "https" && u.Host != "" {
			jar.Source = src.Value
		} else {
			warn(src.Line, "%s: source isn't an https URL; ignored", jar.Name)
		}
	default:
		warn(src.Line, "%s: source isn't an https URL; ignored", jar.Name)
	}
	return jar, true
}

// insideServer says whether name stays inside the server folder.
func insideServer(name string) bool {
	return name != "" && !path.IsAbs(name) && path.Clean(name) == name &&
		name != ".." && !strings.HasPrefix(name, "../") && !strings.Contains(name, "\\")
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
