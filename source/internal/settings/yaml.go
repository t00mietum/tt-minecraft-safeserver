// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Paper, Spigot and Geyser read these files with SnakeYAML, which takes flow
// style, anchors and merges, and keeps the last copy of a key given twice. So
// the check reads them with a YAML parser, and every copy of a key has to be
// right. A fix edits only the lines, to keep the comments and layout, and the
// result is checked again before it's written.

// yamlKey is a nested key to check, such as settings.bungeecord.
type yamlKey struct {
	path      []string
	want      string
	missingOK bool // the server's own default is the wanted value
	ok        func(have string) bool
}

// yamlCopy is one copy of a key. inPlace says its value is a plain scalar on
// the key's own line, reached without an alias or merge, so a fix can edit
// that line.
type yamlCopy struct {
	key, value *yaml.Node
	inPlace    bool
}

// yamlParse reads one YAML document. An empty file has no root.
func yamlParse(text []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(text))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var more yaml.Node
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		// SnakeYAML's load refuses a second document too.
		return nil, errors.New("more than one YAML document")
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	return doc.Content[0], nil
}

func yamlDeref(n *yaml.Node) (*yaml.Node, bool) {
	alias := false
	// An alias can't have an anchor, so this is one step at most.
	for n != nil && n.Kind == yaml.AliasNode {
		n, alias = n.Alias, true
	}
	return n, alias
}

func yamlMergeKey(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!merge"
}

// yamlMerged gives the mappings a merge key's value brings in.
func yamlMerged(v *yaml.Node) []*yaml.Node {
	v, _ = yamlDeref(v)
	if v == nil {
		return nil
	}
	if v.Kind != yaml.SequenceNode {
		return []*yaml.Node{v}
	}
	return v.Content
}

// yamlCopies gives every copy of a nested key: under each copy of its
// parents, and through anchors and merges.
func yamlCopies(root *yaml.Node, path []string) []yamlCopy {
	var found []yamlCopy
	type visit struct {
		n     *yaml.Node
		level int
	}
	seen := map[visit]bool{}
	var walk func(n *yaml.Node, level int, direct bool)
	walk = func(n *yaml.Node, level int, direct bool) {
		n, alias := yamlDeref(n)
		if n == nil || n.Kind != yaml.MappingNode {
			return
		}
		// Merges can bring in one mapping many times over, or loop. Once per
		// level is enough.
		if seen[visit{n, level}] {
			return
		}
		seen[visit{n, level}] = true
		direct = direct && !alias
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			switch {
			case yamlMergeKey(k):
				for _, m := range yamlMerged(v) {
					walk(m, level, false)
				}
			case k.Kind != yaml.ScalarNode || k.Value != path[level]:
			case level == len(path)-1:
				found = append(found, yamlCopy{k, v, direct && v.Kind == yaml.ScalarNode && v.Line == k.Line &&
					(v.Style == 0 || v.Style == yaml.DoubleQuotedStyle || v.Style == yaml.SingleQuotedStyle)})
			default:
				walk(v, level+1, direct)
			}
		}
	}
	walk(root, 0, true)
	return found
}

// yamlLast gives the value SnakeYAML takes for a nested key: the last copy in
// its mapping, or failing that the first merge that has one.
func yamlLast(root *yaml.Node, path []string) *yaml.Node {
	n := root
	for _, name := range path {
		n = yamlChild(n, name, map[*yaml.Node]bool{})
		if n == nil {
			return nil
		}
	}
	n, _ = yamlDeref(n)
	return n
}

// yamlChild looks in each mapping once, since merges can bring one in many
// times over, or loop.
func yamlChild(n *yaml.Node, name string, seen map[*yaml.Node]bool) *yaml.Node {
	n, _ = yamlDeref(n)
	if n == nil || n.Kind != yaml.MappingNode || seen[n] {
		return nil
	}
	seen[n] = true
	var v *yaml.Node
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		switch {
		case yamlMergeKey(k):
			merges = append(merges, yamlMerged(n.Content[i+1])...)
		case k.Kind == yaml.ScalarNode && k.Value == name:
			v = n.Content[i+1]
		}
	}
	for _, m := range merges {
		if v != nil {
			break
		}
		v = yamlChild(m, name, seen)
	}
	return v
}

func yamlShow(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.SequenceNode:
		return "a list"
	}
	return "a section"
}

// yamlScan checks keys in a file's text, and gives the text with them fixed.
// A key that has no copy is added, unless missingOK.
func yamlScan(name string, text []byte, keysFor func(root *yaml.Node) []yamlKey) ([]Problem, []byte, error) {
	root, err := yamlParse(text)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: can't read it as YAML: %s", name, strings.TrimPrefix(err.Error(), "yaml: "))
	}
	str := string(text)
	lines := strings.Split(str, "\n")
	// The parser also counts a lone CR, NEL, LS and PS as line ends, so its
	// line numbers wouldn't match these lines.
	edit := !strings.ContainsAny(strings.ReplaceAll(str, "\r\n", "\n"), "\r\u0085\u2028\u2029")
	var problems []Problem
	var add []yamlKey
	for _, k := range keysFor(root) {
		key := strings.Join(k.path, ".")
		var bad *yaml.Node
		for _, c := range yamlCopies(root, k.path) {
			if c.value.Kind == yaml.ScalarNode && k.ok(c.value.Value) {
				continue
			}
			if bad == nil {
				bad = c.value
			}
			if i := c.key.Line - 1; edit && c.inPlace && i < len(lines) && yamlOnLine(lines[i], c) {
				lines[i] = yamlSet(lines[i], yamlScalar(k.want))
			}
		}
		switch {
		case bad != nil:
			problems = append(problems, Problem{File: name, Key: key, Have: yamlShow(bad), Want: k.want})
		case !k.missingOK && yamlLast(root, k.path) == nil:
			problems = append(problems, Problem{File: name, Key: key, Want: k.want, Missing: true})
			add = append(add, k)
		}
	}
	// Last, since an added line moves the ones after it.
	for _, k := range add {
		lines = yamlInsert(lines, k.path, yamlScalar(k.want))
	}
	return problems, []byte(strings.Join(lines, "\n")), nil
}

// yamlScalar quotes a value that isn't a plain YAML scalar, such as an IPv6
// address.
func yamlScalar(v string) string {
	if strings.Contains(v, ":") {
		return strconv.Quote(v)
	}
	return v
}

// yamlOnLine says the copy is on line as block style writes it: the key first,
// at the parser's column, and then the value.
func yamlOnLine(line string, c yamlCopy) bool {
	if len(line)-len(strings.TrimLeft(line, " ")) != c.key.Column-1 {
		return false
	}
	head, _, _ := yamlSplit(line)
	return strings.Trim(strings.TrimSpace(strings.TrimSuffix(head, ":")), `"'`) == c.key.Value && yamlValue(line) == c.value.Value
}

// yamlFix gives the fixed text, after checking it again. A copy that can't
// be fixed in place, such as one in flow style, leaves the file as it is.
func yamlFix(name string, text []byte, keysFor func(root *yaml.Node) []yamlKey) ([]Problem, []byte, error) {
	problems, fixed, err := yamlScan(name, text, keysFor)
	if err != nil || len(problems) == 0 {
		return problems, nil, err
	}
	left, _, err := yamlScan(name, fixed, keysFor)
	if err == nil && len(left) == 0 {
		return problems, fixed, nil
	}
	if err != nil {
		left = problems
	}
	var names []string
	for _, p := range left {
		names = append(names, p.Key)
	}
	return nil, nil, fmt.Errorf("%s: can't set %s in this file's layout, so it's left as it is; set it by hand", name, strings.Join(names, ", "))
}

// yamlFind gives the line of a nested key, such as settings.bungeecord, in
// block style. Only yamlInsert uses it, and the result is checked again.
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
