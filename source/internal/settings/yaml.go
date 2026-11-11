// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import "strings"

// Paper and Spigot write block-style YAML, a key per line, nested by spaces.
// That's all these read, so no YAML library is needed for 3 settings.

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
