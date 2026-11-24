// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"strconv"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
)

// Geyser's config folder for the Spigot plugin. A missing config is wrong
// too: Geyser's first start writes its defaults, with online mode logins on
// 19132, and runs with them. Given only these keys, it fills in the rest.
var geyserDirs = []string{"plugins", "Geyser-Spigot"}

const geyserFile = "config.yml"

func exactly(want string) func(string) bool {
	return func(have string) bool { return have == want }
}

func anyCase(want string) func(string) bool {
	return func(have string) bool { return strings.EqualFold(have, want) }
}

func geyserSettings(c *config.Config, root *yaml.Node) []yamlKey {
	addr := c.BindIP
	if addr == "" {
		addr = "0.0.0.0"
	}
	port := strconv.Itoa(c.BedrockPort)
	return []yamlKey{
		{path: []string{"bedrock", "address"}, want: addr, ok: exactly(addr)},
		{path: []string{"bedrock", "port"}, want: port, ok: exactly(port)},
		{path: geyserAuthPath(root), want: "floodgate", ok: anyCase("floodgate")},
		// Keep NetherNet on bedrockPort alone, TCP and UDP, as the firewall
		// and the forward advice expect. See design.md, Bedrock players.
		{path: []string{"bedrock", "transport"}, want: "nethernet", ok: anyCase("nethernet"), missingOK: true},
		{path: []string{"bedrock", "webrtc-port"}, want: "0", ok: exactly("0"), missingOK: true},
		{path: []string{"bedrock", "clone-remote-port"}, want: "false", ok: isFalse, missingOK: true},
	}
}

// geyserAuthPath finds auth-type for the layout the file is in. Geyser's
// config-version 5 renamed remote to java, and before remote it sat at the top.
// Geyser moves an old file to the new layout as it loads it.
func geyserAuthPath(root *yaml.Node) []string {
	if v := yamlLast(root, []string{"config-version"}); v != nil {
		if n, err := strconv.Atoi(v.Value); err == nil && n >= 5 {
			return []string{"java", "auth-type"}
		}
	}
	if yamlLast(root, []string{"remote"}) != nil {
		return []string{"remote", "auth-type"}
	}
	if yamlLast(root, []string{"auth-type"}) != nil {
		return []string{"auth-type"}
	}
	return []string{"java", "auth-type"}
}

func scanGeyser(top dir, c *config.Config, fix bool) ([]Problem, error) {
	name := strings.Join(geyserDirs, "/") + "/" + geyserFile
	d, there := top, true
	for _, sub := range geyserDirs {
		next, err := d.Sub(sub)
		if isNotExist(err) && fix {
			next, err = d.Mkdir(sub, nil)
		}
		if isNotExist(err) {
			there = false
			break
		}
		if err != nil {
			return nil, err
		}
		defer next.Close()
		d = next
	}
	var text []byte
	var st *syscall.Stat_t
	if there {
		var err error
		text, st, err = d.Read(geyserFile)
		if err != nil && !isNotExist(err) {
			return nil, err
		}
	}
	keysFor := func(root *yaml.Node) []yamlKey { return geyserSettings(c, root) }
	if !fix {
		problems, _, err := yamlScan(name, text, keysFor)
		return problems, err
	}
	problems, fixed, err := yamlFix(name, text, keysFor)
	if err != nil || len(problems) == 0 {
		return problems, err
	}
	return problems, d.Write(geyserFile, fixed, st)
}
