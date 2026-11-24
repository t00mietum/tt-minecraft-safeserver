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
	end        string // the line end after text, empty for a last line with none
	key, value string
	entry      bool
}

// parseProperties reads lines as Java's Properties.load does: a line ends at
// LF, CR or CRLF, and a comment never continues onto the next. A # or ! is a
// comment wherever the logical line is still empty, even after a backslash.
func parseProperties(b []byte) properties {
	var p properties
	phys := splitLines(string(b))
	for i := 0; i < len(phys); i++ {
		text, end := phys[i].text, phys[i].end
		logical := ""
		for {
			seg := strings.TrimLeft(phys[i].text, " \t\f")
			if logical == "" && seg != "" && (seg[0] == '#' || seg[0] == '!') {
				logical = seg
				break
			}
			logical += seg
			if !continues(logical) || i+1 == len(phys) {
				break
			}
			logical = logical[:len(logical)-1]
			i++
			text += end + phys[i].text
			end = phys[i].end
		}
		l := parseLine(text, logical)
		l.end = end
		p.lines = append(p.lines, l)
	}
	return p
}

type physLine struct{ text, end string }

func splitLines(s string) []physLine {
	var lines []physLine
	for s != "" {
		i := strings.IndexAny(s, "\r\n")
		if i < 0 {
			lines = append(lines, physLine{s, ""})
			break
		}
		n := 1
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			n = 2
		}
		lines = append(lines, physLine{s[:i], s[i : i+n]})
		s = s[i+n:]
	}
	return lines
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
// a setting here can have, such as the colons in an IPv6 address.
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
	line := propLine{text: key + "=" + escape(value), end: "\n", key: key, value: value, entry: true}
	found := false
	for i, l := range p.lines {
		if l.entry && l.key == key {
			line.end = l.end
			p.lines[i] = line
			found = true
		}
	}
	if !found {
		// A last line that ends in a backslash would take the new one as its
		// continuation, so a blank line goes between. Its end is the same as the
		// line before, since an LF after a lone CR would only make a CRLF.
		if n := len(p.lines); n > 0 && p.lines[n-1].entry && continues(p.lines[n-1].text) {
			blank := propLine{end: p.lines[n-1].end}
			if blank.end == "" {
				blank.end = "\n"
			}
			p.lines = append(p.lines, blank)
		}
		p.lines = append(p.lines, line)
	}
}

// bytes gives the file as it was read, with a line end added to a last line
// that has none.
func (p properties) bytes() []byte {
	var b strings.Builder
	for _, l := range p.lines {
		b.WriteString(l.text)
		if l.end == "" {
			l.end = "\n"
		}
		b.WriteString(l.end)
	}
	return []byte(b.String())
}
