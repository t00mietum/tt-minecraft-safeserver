// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// tt-minecraftd runs a Paper server locked down, and checks it at every start.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime/debug"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
)

// version is set at release with -ldflags "-X main.version=...".
var version = "v1.0.0-alpha.1"

// defaultConfig is a var so tests don't read the host's real config.
var defaultConfig = config.DefaultPath

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type options struct {
	configPath string
	sets       []config.Option
	help       bool
	debug      bool
	dryRun     bool
	foreground bool
	warn       int
	interval   int
}

type command struct {
	name    string
	summary string
	flags   func(fs *flag.FlagSet, o *options)
}

var commands = []command{
	{"start", "Check the settings and start the server", func(fs *flag.FlagSet, o *options) {
		fs.BoolFunc("native", "Run in native mode, the same as --set runMode=native", func(string) error {
			o.sets = append(o.sets, config.Option{Key: "runMode", Value: "native"})
			return nil
		})
		fs.BoolVar(&o.debug, "debug", false, "Run in the foreground with the console attached")
		fs.BoolVar(&o.dryRun, "dry-run", false, "Print the commands that would change something, and run none of them")
		fs.BoolVar(&o.foreground, "foreground", false, "Stay attached until the server exits, for the boot unit")
	}},
	{"stop", "Stop the server and hand the trees back", func(fs *flag.FlagSet, o *options) {
		fs.IntVar(&o.warn, "warn", 0, "Count down `N` seconds for the players first")
		fs.BoolVar(&o.dryRun, "dry-run", false, "Print the commands that would change something, and run none of them")
	}},
	{"status", "Show what the server is doing", func(*flag.FlagSet, *options) {}},
	{"watch", "Redraw status every few seconds, until Ctrl-C", func(fs *flag.FlagSet, o *options) {
		fs.IntVar(&o.interval, "interval", 2, "Seconds `N` between redraws")
	}},
}

// invocation is a parsed command line with its config loaded.
type invocation struct {
	cmd  *command
	opts options
	cfg  config.Config
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	inv, code, done := prepare(args, stdout, stderr)
	if done {
		return code
	}
	// Each command's own item builds it. Until then it says so, rather than
	// doing half of a start or stop.
	fmt.Fprintf(stderr, "tt-minecraftd: %s isn't built yet\n", inv.cmd.name)
	return exitError
}

// commonFlags is called twice, before the command and after it, so a value
// from the first pass is kept as the default for the second.
func commonFlags(fs *flag.FlagSet, o *options) {
	fs.StringVar(&o.configPath, "config", o.configPath, "Config file `PATH`, instead of "+defaultConfig)
	fs.Func("set", "Override one config key, as `KEY=VALUE`. Can be given more than once", func(s string) error {
		key, value, ok := strings.Cut(s, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return fmt.Errorf("%q isn't KEY=VALUE", s)
		}
		o.sets = append(o.sets, config.Option{Key: strings.TrimSpace(key), Value: value})
		return nil
	})
	fs.BoolVar(&o.help, "help", false, "Show this help")
	fs.BoolVar(&o.help, "h", false, "Show this help")
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// prepare parses args and loads the config. done is true when there's nothing
// left to run, such as after --help or an error, and code is the exit code.
// Common options may come before or after the command.
func prepare(args []string, stdout, stderr io.Writer) (inv invocation, code int, done bool) {
	usageErr := func(format string, a ...any) (invocation, int, bool) {
		fmt.Fprintf(stderr, "tt-minecraftd: "+format+"\nSee tt-minecraftd --help.\n", a...)
		return invocation{}, exitUsage, true
	}
	var o options
	var about, showVersion bool
	top := newFlagSet("tt-minecraftd")
	commonFlags(top, &o)
	top.BoolVar(&about, "about", false, "")
	top.BoolVar(&showVersion, "version", false, "")
	if err := top.Parse(args); err != nil {
		return usageErr("%v", err)
	}
	switch {
	case o.help:
		writeHelp(stdout)
		return inv, exitOK, true
	case about:
		writeAbout(stdout)
		return inv, exitOK, true
	case showVersion:
		fmt.Fprintf(stdout, "tt-minecraftd %s (build %s)\n", version, build())
		return inv, exitOK, true
	case top.NArg() == 0:
		writeHelp(stderr)
		return inv, exitUsage, true
	}

	name := top.Arg(0)
	for i := range commands {
		if commands[i].name == name {
			inv.cmd = &commands[i]
		}
	}
	if inv.cmd == nil {
		return usageErr("unknown command %q", name)
	}
	cmdFlags := newFlagSet(name)
	commonFlags(cmdFlags, &o)
	inv.cmd.flags(cmdFlags, &o)
	if err := cmdFlags.Parse(top.Args()[1:]); err != nil {
		return usageErr("%s: %v", name, err)
	}
	if o.help {
		writeCommandHelp(stdout, inv.cmd, cmdFlags)
		return inv, exitOK, true
	}
	switch {
	case cmdFlags.NArg() > 0:
		return usageErr("%s: unexpected %q", name, cmdFlags.Arg(0))
	case o.debug && o.foreground:
		return usageErr("start: --debug and --foreground don't go together")
	case o.warn < 0:
		return usageErr("stop: --warn takes 0 or more seconds")
	case name == "watch" && o.interval < 1:
		return usageErr("watch: --interval takes 1 or more seconds")
	}
	inv.opts = o

	cfg, warns, err := loadConfig(o, stderr)
	var pathErr *fs.PathError
	switch {
	case errors.As(err, &pathErr):
		fmt.Fprintf(stderr, "tt-minecraftd: %v\n", err)
		return inv, exitError, true
	case err != nil:
		return usageErr("%v", err)
	}
	for _, w := range warns {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	inv.cfg = cfg
	return inv, exitOK, false
}

// loadConfig reads --config, or the default file. A missing default file means
// the defaults, since status and the like should work before setup. A missing
// --config file is an error, since it was named just now.
func loadConfig(o options, stderr io.Writer) (config.Config, []config.Warning, error) {
	if o.configPath != "" {
		return config.Load(o.configPath, o.sets)
	}
	cfg, warns, err := config.Load(defaultConfig, o.sets)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "warning: no config at %s, so the defaults are used; setup writes one\n", defaultConfig)
		return config.Load("", o.sets)
	}
	return cfg, warns, err
}

// build is the commit the binary was built from, when go build recorded one.
func build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	rev = rev[:min(12, len(rev))]
	if dirty {
		rev += "-dirty"
	}
	return rev
}

func writeHelp(w io.Writer) {
	var b strings.Builder
	b.WriteString("\nUsage:\n  tt-minecraftd <command> [options]\n  tt-minecraftd --help | --about | --version\n\nCommands:\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-8s %s\n", c.name, c.summary)
	}
	b.WriteString("\nOptions for every command:\n")
	fs := newFlagSet("")
	var o options
	commonFlags(fs, &o)
	writeFlags(&b, fs)
	b.WriteString("\nSee tt-minecraftd <command> --help for its own options.\n\n")
	fmt.Fprint(w, b.String())
}

func writeCommandHelp(w io.Writer, c *command, fs *flag.FlagSet) {
	var b strings.Builder
	fmt.Fprintf(&b, "\nUsage:\n  tt-minecraftd %s [options]\n\n%s.\n\nOptions:\n", c.name, c.summary)
	writeFlags(&b, fs)
	b.WriteString("\n")
	fmt.Fprint(w, b.String())
}

func writeFlags(b *strings.Builder, fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		if f.Name == "h" {
			return
		}
		arg, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(b, "  %-20s %s\n", strings.TrimSpace("--"+f.Name+" "+arg), usage)
	})
}

func writeAbout(w io.Writer) {
	fmt.Fprint(w, `
tt-minecraftd runs a Paper Minecraft server for friends, locked down. The
server runs in a container, or in a systemd sandbox, as its own account. Every
start checks the safe settings and the plugin jars first.

Copyright © 2026 t00mietum. GPL v2 or later.
https://github.com/t00mietum/tt-minecraft-safeserver

`)
}
