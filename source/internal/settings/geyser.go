// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
)

// Geyser's config, as the Spigot plugin keeps it. A missing one is left
// alone: Geyser writes it with its defaults at its first start, which bind
// all addresses and log in with online mode, and the next start checks it.
var geyserDirs = []string{"plugins", "Geyser-Spigot"}

const geyserFile = "config.yml"

// geyserSetting is one key under Geyser's config, and how to tell its value.
type geyserSetting struct {
	path       []string
	want       string
	ignoreCase bool
	missingOK  bool // Geyser's own default is the wanted value
}

func (s geyserSetting) ok(have string) bool {
	if s.ignoreCase {
		return strings.EqualFold(have, s.want)
	}
	return have == s.want
}

func geyserSettings(c *config.Config, lines []string) []geyserSetting {
	addr := c.BindIP
	if addr == "" {
		addr = "0.0.0.0"
	}
	return []geyserSetting{
		{path: []string{"bedrock", "address"}, want: addr},
		{path: []string{"bedrock", "port"}, want: strconv.Itoa(c.BedrockPort)},
		{path: geyserAuthPath(lines), want: "floodgate", ignoreCase: true},
		// Keep NetherNet on bedrockPort alone, TCP and UDP, as the firewall
		// and the forward advice expect. See design.md, Bedrock players.
		{path: []string{"bedrock", "transport"}, want: "nethernet", ignoreCase: true, missingOK: true},
		{path: []string{"bedrock", "webrtc-port"}, want: "0", missingOK: true},
		{path: []string{"bedrock", "clone-remote-port"}, want: "false", ignoreCase: true, missingOK: true},
	}
}

// geyserAuthPath finds auth-type for the layout the file is in. Geyser's
// config-version 5 renamed remote to java, and before remote it sat at the top.
// Geyser moves an old file to the new layout as it loads it.
func geyserAuthPath(lines []string) []string {
	if i, ok := yamlFind(lines, []string{"config-version"}); ok {
		if v, err := strconv.Atoi(yamlValue(lines[i])); err == nil && v >= 5 {
			return []string{"java", "auth-type"}
		}
	}
	if _, ok := yamlFind(lines, []string{"remote"}); ok {
		return []string{"remote", "auth-type"}
	}
	if _, ok := yamlFind(lines, []string{"auth-type"}); ok {
		return []string{"auth-type"}
	}
	return []string{"java", "auth-type"}
}

func scanGeyser(top dir, c *config.Config, fix bool) ([]Problem, error) {
	d := top
	for _, name := range geyserDirs {
		sub, err := d.sub(name)
		if isNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer sub.close()
		d = sub
	}
	name := strings.Join(append(geyserDirs[:len(geyserDirs):len(geyserDirs)], geyserFile), "/")
	text, st, err := d.read(geyserFile)
	if isNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(text), "\n")
	var problems []Problem
	for _, s := range geyserSettings(c, lines) {
		key := strings.Join(s.path, ".")
		value := s.want
		if strings.Contains(value, ":") {
			value = strconv.Quote(value) // an IPv6 address isn't a plain YAML scalar
		}
		i, found := yamlFind(lines, s.path)
		if !found && s.missingOK {
			continue
		}
		if !found {
			problems = append(problems, Problem{File: name, Key: key, Want: s.want, Missing: true})
			lines = yamlInsert(lines, s.path, value)
			continue
		}
		have := yamlValue(lines[i])
		if s.ok(have) {
			continue
		}
		problems = append(problems, Problem{File: name, Key: key, Have: have, Want: s.want})
		lines[i] = yamlSet(lines[i], value)
	}
	if !fix || len(problems) == 0 {
		return problems, nil
	}
	return problems, d.write(geyserFile, []byte(strings.Join(lines, "\n")), st)
}
