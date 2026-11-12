// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yottacore/shcl/source/go/v3"
)

// DefaultPath is where setup writes the config. --config points elsewhere.
const DefaultPath = "/etc/tt-minecraftd/config.shcl"

const gib = 1 << 30

// Defaults gives the settings that don't depend on other keys. The rest,
// such as runGroup and memCap, are left empty for Load to fill in last.
func Defaults() Config {
	return Config{
		RunMode:   "podman",
		RunUser:   "minecraftd",
		ServerDir: "/srv/tt-minecraftd/server",
		Image:     "docker.io/library/eclipse-temurin:25-jre@sha256:ecdc0adb37bde1a281c4d245a3cd535004b1fc8322d560f5b09463b248c81df0",
		JavaBin:   "java",
		HeapMax:   4 * gib,
		// Paper's recommended flags, from docs.papermc.io/paper/aikars-flags.
		JvmArgs: []string{
			"-XX:+UseG1GC",
			"-XX:+ParallelRefProcEnabled",
			"-XX:MaxGCPauseMillis=200",
			"-XX:+UnlockExperimentalVMOptions",
			"-XX:+DisableExplicitGC",
			"-XX:+AlwaysPreTouch",
			"-XX:G1NewSizePercent=30",
			"-XX:G1MaxNewSizePercent=40",
			"-XX:G1HeapRegionSize=8M",
			"-XX:G1ReservePercent=20",
			"-XX:G1HeapWastePercent=5",
			"-XX:G1MixedGCCountTarget=4",
			"-XX:InitiatingHeapOccupancyPercent=15",
			"-XX:G1MixedGCLiveThresholdPercent=90",
			"-XX:G1RSetUpdatingPauseTimePercent=5",
			"-XX:SurvivorRatio=32",
			"-XX:+PerfDisableSharedMem",
			"-XX:MaxTenuringThreshold=1",
			"-Dusing.aikars.flags=https://mcflags.emc.gs",
			"-Daikars.new.flags=true",
		},
		GamePort:          25565,
		Bedrock:           true,
		BedrockPort:       19132,
		AllowFrom:         []string{"lan"},
		JoinRateLimit:     20,
		Egress:            "web",
		HideOnlinePlayers: true,
		StartTimeout:      300 * time.Second,
		StopTimeout:       120 * time.Second,
		Firewall:          true,
	}
}

// Option is a setting from the command line, such as runMode from --native.
// It wins over the file.
type Option struct {
	Key   string
	Value string
}

// Load builds the settings: the defaults, then the file at path, then the
// options, then the keys that come from others. An empty path skips the file.
// A bad line in the file is a warning, but a file that can't be read, or a
// bad option, is an error, since the operator typed it just now.
func Load(path string, opts []Option) (Config, []Warning, error) {
	c := Defaults()
	var warns []Warning
	if path != "" {
		var err error
		if warns, err = Read(path, &c); err != nil {
			return Config{}, nil, err
		}
	}
	for _, o := range opts {
		if err := c.Set(o.Key, o.Value); err != nil {
			return Config{}, warns, err
		}
	}
	warns = append(warns, c.derive()...)
	return c, warns, nil
}

// Set sets one key from text written as it would be in the file, such as
// "6G" or "[lan, 203.0.113.7]". Unlike in the file, a bad value is an error.
func (c *Config) Set(key, value string) error {
	set, known := c.setters()[strings.ToLower(key)]
	if !known {
		return fmt.Errorf("unknown key %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s: takes one line", key)
	}
	doc := shcl.Parse(key + ": " + value + "\n")
	for _, d := range doc.Diagnostics() {
		if d.Severity == shcl.SeverityError {
			return fmt.Errorf("%s: %s", key, d.Message)
		}
	}
	tops := doc.ReadChildFields("").Value
	if len(tops) != 1 {
		return fmt.Errorf("%s: takes one value or one list", key)
	}
	if err := set(doc, tops[0].Path); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

// derive fills the keys whose defaults come from others, and settles keys
// that clash. It runs after the options, so --set runUser=x moves runGroup too.
func (c *Config) derive() []Warning {
	var warns []Warning
	if c.RunGroup == "" {
		c.RunGroup = c.RunUser
	}
	if c.OwnerAfterStop == "" {
		c.OwnerAfterStop = operator()
	}
	if len(c.OwnerTrees) == 0 {
		c.OwnerTrees = []string{c.ServerDir}
	}
	switch {
	case c.HeapMin == 0:
		c.HeapMin = min(gib, c.HeapMax)
	case c.HeapMin > c.HeapMax:
		warns = append(warns, Warning{Text: fmt.Sprintf("heapMin %s is over heapMax %s; using %[2]s for both", javaForm(c.HeapMin), javaForm(c.HeapMax))})
		c.HeapMin = c.HeapMax
	}
	// JVM overhead on top of the heap would hit a cap this low right away.
	if c.MemCap != 0 && c.MemCap <= c.HeapMax {
		warns = append(warns, Warning{Text: fmt.Sprintf("memCap %s isn't over heapMax %s; using the default", javaForm(c.MemCap), javaForm(c.HeapMax))})
		c.MemCap = 0
	}
	if c.MemCap == 0 {
		c.MemCap = c.HeapMax + c.HeapMax/4 + gib
	}
	// Geyser's signaling needs bedrockPort over TCP. When Paper holds it,
	// Geyser sends Bedrock players through an outside service instead.
	if c.Bedrock && c.BedrockPort == c.GamePort {
		port := 19132
		if port == c.GamePort {
			port++
		}
		warns = append(warns, Warning{Text: fmt.Sprintf("bedrockPort %d is gamePort too; using %d", c.BedrockPort, port)})
		c.BedrockPort = port
	}
	return warns
}

// operator is who gets the trees back after a stop. It's empty when neither
// variable holds a usable name, as when systemd starts the boot unit.
func operator() string {
	for _, name := range []string{"SUDO_USER", "USER"} {
		if s, err := checkAccount(os.Getenv(name)); err == nil {
			return s
		}
	}
	return ""
}

// javaForm prints a size the way it would be typed, such as 4G or 1536M.
func javaForm(n int64) string {
	for _, u := range []struct {
		shift  uint
		suffix string
	}{{40, "T"}, {30, "G"}, {20, "M"}, {10, "K"}} {
		if n >= 1<<u.shift && n%(1<<u.shift) == 0 {
			return fmt.Sprintf("%d%s", n>>u.shift, u.suffix)
		}
	}
	return fmt.Sprint(n)
}
