// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package settings checks and fixes the safe server settings, per design.md's
// Safe server settings.
package settings

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
)

// Problem is one setting that isn't safe.
type Problem struct {
	File    string // under serverDir
	Key     string
	Have    string
	Want    string
	Missing bool
}

func (p Problem) String() string {
	if p.Missing {
		return fmt.Sprintf("%s: %s isn't set, and should be %s", p.File, p.Key, show(p.Want))
	}
	return fmt.Sprintf("%s: %s is %s, and should be %s", p.File, p.Key, show(p.Have), show(p.Want))
}

// Fixed says what Fix did about it.
func (p Problem) Fixed() string {
	if p.Missing {
		return fmt.Sprintf("%s: %s is now %s", p.File, p.Key, show(p.Want))
	}
	return fmt.Sprintf("%s: %s is now %s, was %s", p.File, p.Key, show(p.Want), show(p.Have))
}

func show(v string) string {
	if v == "" {
		return "empty"
	}
	return strconv.Quote(v)
}

// rule is one setting and its safe value. A bool compares without case, as
// Java's Boolean.valueOf reads it.
type rule struct {
	key, want string
	isBool    bool
}

func (r rule) ok(have string) bool {
	if r.isBool {
		return strings.EqualFold(have, r.want)
	}
	return have == r.want
}

func on(key string) rule  { return rule{key, "true", true} }
func off(key string) rule { return rule{key, "false", true} }

func propertyRules(c *config.Config) []rule {
	return []rule{
		on("online-mode"),
		on("white-list"),
		on("enforce-whitelist"),
		off("enable-rcon"),
		off("enable-query"),
		off("management-server-enabled"),
		off("enable-jmx-monitoring"),
		{key: "server-ip", want: c.BindIP},
		{key: "server-port", want: strconv.Itoa(c.GamePort)},
		{key: "hide-online-players", want: strconv.FormatBool(c.HideOnlinePlayers), isBool: true},
	}
}

// yamlSetting is a dotted path in a Paper YAML file. A missing file or key is
// fine, since each one defaults to off.
type yamlSetting struct {
	dir, file string
	path      []string
}

var yamlSettings = []yamlSetting{
	{"", "spigot.yml", []string{"settings", "bungeecord"}},
	{"config", "paper-global.yml", []string{"proxies", "velocity", "enabled"}},
	// A client could send its own PROXY header and claim any address.
	{"config", "paper-global.yml", []string{"proxies", "proxy-protocol"}},
}

// Check lists the settings that aren't safe. It changes nothing.
func Check(c *config.Config) ([]Problem, error) {
	return scan(c, false)
}

// Fix sets what Check would list, and gives what it changed. server.properties
// is written when it's missing, so the first start isn't open to all.
func Fix(c *config.Config) ([]Problem, error) {
	return scan(c, true)
}

func scan(c *config.Config, fix bool) ([]Problem, error) {
	top, err := openDir(c.ServerDir)
	if err != nil {
		return nil, err
	}
	defer top.Close()
	problems, err := scanProperties(top, propertyRules(c), fix)
	if err != nil {
		return problems, err
	}
	byFile := map[string][]yamlSetting{}
	var order []string
	for _, s := range yamlSettings {
		name := joinName(s.dir, s.file)
		if _, seen := byFile[name]; !seen {
			order = append(order, name)
		}
		byFile[name] = append(byFile[name], s)
	}
	// A YAML file that can't be read or fixed doesn't stop the others.
	var errs []error
	for _, name := range order {
		found, err := scanYAML(top, byFile[name], fix)
		problems = append(problems, found...)
		errs = append(errs, err)
	}
	if c.Bedrock {
		found, err := scanGeyser(top, c, fix)
		problems = append(problems, found...)
		errs = append(errs, err)
	}
	return problems, errors.Join(errs...)
}

func joinName(dir, file string) string {
	if dir == "" {
		return file
	}
	return dir + "/" + file
}

const propertiesFile = "server.properties"

func scanProperties(top dir, rules []rule, fix bool) ([]Problem, error) {
	text, st, err := top.Read(propertiesFile)
	missingFile := isNotExist(err)
	if err != nil && !missingFile {
		return nil, err
	}
	props := parseProperties(text)
	var problems []Problem
	for _, r := range rules {
		have, ok := props.get(r.key)
		switch {
		case !ok:
			problems = append(problems, Problem{File: propertiesFile, Key: r.key, Want: r.want, Missing: true})
		case !r.ok(have):
			problems = append(problems, Problem{File: propertiesFile, Key: r.key, Have: have, Want: r.want})
		}
	}
	if !fix || len(problems) == 0 {
		return problems, nil
	}
	for _, p := range problems {
		props.set(p.Key, p.Want)
	}
	return problems, top.Write(propertiesFile, props.bytes(), st)
}

func isFalse(have string) bool { return strings.EqualFold(have, "false") }

func scanYAML(top dir, list []yamlSetting, fix bool) ([]Problem, error) {
	d := top
	if list[0].dir != "" {
		sub, err := top.Sub(list[0].dir)
		if isNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer sub.Close()
		d = sub
	}
	name := joinName(list[0].dir, list[0].file)
	text, st, err := d.Read(list[0].file)
	if isNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keysFor := func(*yaml.Node) []yamlKey {
		keys := make([]yamlKey, 0, len(list))
		for _, s := range list {
			keys = append(keys, yamlKey{path: s.path, want: "false", missingOK: true, ok: isFalse})
		}
		return keys
	}
	if !fix {
		problems, _, err := yamlScan(name, text, keysFor)
		return problems, err
	}
	problems, fixed, err := yamlFix(name, text, keysFor)
	if err != nil || len(problems) == 0 {
		return problems, err
	}
	return problems, d.Write(list[0].file, fixed, st)
}
