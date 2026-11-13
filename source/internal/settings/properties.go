// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"strconv"
	"strings"
)

// properties is a Java properties file kept line by line, so a fix leaves
// the other lines and comments as they were.
type properties struct {
	lines []propLine
}

type propLine struct {
	text       string // as in the file, continuations and all
	key, value string
	entry      bool
}

func parseProperties(b []byte) properties {
	var p properties
	phys := strings.Split(string(b), "\n")
	if phys[len(phys)-1] == "" {
		phys = phys[:len(phys)-1]
	}
	for i := 0; i < len(phys); i++ {
		text := phys[i]
		logical := strings.TrimSuffix(text, "\r")
		for continues(logical) && i+1 < len(phys) {
			i++
			text += "\n" + phys[i]
			logical = logical[:len(logical)-1] + strings.TrimLeft(strings.TrimSuffix(phys[i], "\r"), " \t\f")
		}
		p.lines = append(p.lines, parseLine(text, logical))
	}
	return p
}

// continues says the line ends in an odd number of backslashes.
func continues(s string) bool {
	n := len(s) - len(strings.TrimRight(s, `\`))
	return n%2 == 1
}

func parseLine(text, s string) propLine {
	s = strings.TrimLeft(s, " \t\f")
	if s == "" || s[0] == '#' || s[0] == '!' {
		return propLine{text: text}
	}
	end := 0
	for end < len(s) {
		c := s[end]
		if c == '\\' {
			end += 2
			continue
		}
		if strings.IndexByte("=: \t\f", c) >= 0 {
			break
		}
		end++
	}
	end = min(end, len(s))
	rest := strings.TrimLeft(s[end:], " \t\f")
	if rest != "" && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t\f")
	}
	return propLine{text: text, key: unescape(s[:end]), value: unescape(rest), entry: true}
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i == len(s) {
			break
		}
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if r, err := strconv.ParseUint(s[i+1:min(i+5, len(s))], 16, 16); err == nil && i+5 <= len(s) {
				b.WriteRune(rune(r))
				i += 4
				continue
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// escape writes a value as Java's Properties.store would, for the characters
// a setting here can hold, such as the colons in an IPv6 address.
func escape(s string) string {
	var b strings.Builder
	for i, c := range s {
		switch {
		case c == ' ' && i == 0:
			b.WriteString(`\ `)
		case strings.ContainsRune(`\=:#!`, c):
			b.WriteByte('\\')
			b.WriteRune(c)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\f':
			b.WriteString(`\f`)
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// get gives the last value for key, since Java keeps the last one.
func (p properties) get(key string) (string, bool) {
	for i := len(p.lines) - 1; i >= 0; i-- {
		if l := p.lines[i]; l.entry && l.key == key {
			return l.value, true
		}
	}
	return "", false
}

// set changes every line for key, or adds one at the end.
func (p *properties) set(key, value string) {
	line := propLine{text: key + "=" + escape(value), key: key, value: value, entry: true}
	found := false
	for i, l := range p.lines {
		if l.entry && l.key == key {
			if strings.HasSuffix(l.text, "\r") {
				line.text += "\r"
			}
			p.lines[i] = line
			found = true
		}
	}
	if !found {
		// A last line that ends in a backslash would take the new one as its
		// continuation, so a blank line goes between.
		if n := len(p.lines); n > 0 && continues(strings.TrimSuffix(p.lines[n-1].text, "\r")) {
			p.lines = append(p.lines, propLine{})
		}
		p.lines = append(p.lines, line)
	}
}

func (p properties) bytes() []byte {
	var b strings.Builder
	for _, l := range p.lines {
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
