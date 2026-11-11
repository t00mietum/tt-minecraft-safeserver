// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import "strings"

// Paper and Spigot write block-style YAML, a key per line, nested by spaces.
// Geyser writes the same. That's all these read, so no YAML library is needed
// for a handful of settings.

// yamlFind gives the line of a nested key, such as settings.bungeecord.
func yamlFind(lines []string, path []string) (int, bool) {
	parent, child, level := -1, -1, 0
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if t == "" || t[0] == '#' || t[0] == '\r' {
			continue
		}
		indent := len(l) - len(t)
		if indent <= parent {
			return 0, false
		}
		if child < 0 {
			child = indent
		}
		if indent != child {
			continue
		}
		key, _, ok := strings.Cut(t, ":")
		if !ok || strings.Trim(strings.TrimSpace(key), `"'`) != path[level] {
			continue
		}
		if level == len(path)-1 {
			return i, true
		}
		level++
		parent, child = indent, -1
	}
	return 0, false
}

// yamlSplit cuts a key line into the key and colon, the value, and the rest,
// which is any comment and line end.
func yamlSplit(line string) (head, value, tail string) {
	colon := strings.IndexByte(line, ':') + 1
	head, rest := line[:colon], line[colon:]
	end := len(rest)
	if i := strings.Index(rest, " #"); i >= 0 {
		end = i
	}
	value = strings.TrimRight(rest[:end], " \r")
	return head, value, rest[len(value):]
}

func yamlValue(line string) string {
	_, v, _ := yamlSplit(line)
	return strings.Trim(strings.TrimSpace(v), `"'`)
}

func yamlSet(line, value string) string {
	head, _, tail := yamlSplit(line)
	return head + " " + value + tail
}

// yamlInsert adds a key that yamlFind didn't find, at the end of its parent's
// block, or at the end of the file with its parent when that's missing too.
// Only one or two levels, which is all Geyser's keys need.
func yamlInsert(lines []string, path []string, value string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	var add []string
	at := end
	switch len(path) {
	case 1:
		add = []string{path[0] + ": " + value}
	case 2:
		parent, ok := yamlFind(lines, path[:1])
		if !ok {
			add = []string{path[0] + ":", "  " + path[1] + ": " + value}
			break
		}
		parentIndent := len(lines[parent]) - len(strings.TrimLeft(lines[parent], " "))
		indent := ""
		at = parent + 1
		for i := parent + 1; i < len(lines); i++ {
			t := strings.TrimLeft(lines[i], " ")
			if t == "" || t[0] == '#' || t[0] == '\r' {
				continue
			}
			n := len(lines[i]) - len(t)
			if n <= parentIndent {
				break
			}
			if indent == "" {
				indent = lines[i][:n]
			}
			at = i + 1
		}
		if indent == "" {
			indent = strings.Repeat(" ", parentIndent+2)
		}
		return splice(lines, at, indent+path[1]+": "+value)
	default:
		panic("yamlInsert: path too deep")
	}
	return splice(lines, at, add...)
}

func splice(lines []string, at int, add ...string) []string {
	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:at]...)
	out = append(out, add...)
	return append(out, lines[at:]...)
}
