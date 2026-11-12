// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// tt-minecraftd runs a Paper server locked down, and checks it at every start.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	repairpkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/repair"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/status"
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
	check      bool
	approve    bool
	// line is cmd's console command.
	line string
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
	{"repair", "Check the setup, and fix what's wrong", func(fs *flag.FlagSet, o *options) {
		fs.BoolVar(&o.check, "check", false, "Only report, and change nothing. Doesn't need root")
		fs.BoolVar(&o.approve, "approve-plugins", false, "Show each new or changed jar, and ask whether to approve it")
	}},
	{"status", "Show what the server is doing", func(*flag.FlagSet, *options) {}},
	{"watch", "Redraw status every few seconds, until Ctrl-C", func(fs *flag.FlagSet, o *options) {
		fs.IntVar(&o.interval, "interval", 2, "Seconds `N` between redraws")
	}},
	{"console", "Attach to the server console. Ctrl-C leaves it running", func(*flag.FlagSet, *options) {}},
	{"cmd", "Send one console command, and print what the server says back", func(*flag.FlagSet, *options) {}},
}

// invocation is a parsed command line with its config loaded.
type invocation struct {
	cmd  *command
	opts options
	cfg  config.Config
	// configPath is the file the config came from, or would have.
	configPath string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	inv, code, done := prepare(args, stdout, stderr)
	if done {
		return code
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if err := dispatch(ctx, inv, stdout); err != nil {
		fmt.Fprintf(stderr, "tt-minecraftd: %v\n", err)
		return exitError
	}
	return exitOK
}

func dispatch(ctx context.Context, inv invocation, stdout io.Writer) error {
	name := inv.cmd.name
	if name == "status" || name == "watch" {
		runner := host.NewExec(false)
		runner.QuietErr = true
		probe := status.NewProbe(runner, &inv.cfg, inv.pluginsFile())
		if name == "status" {
			width, _ := terminal(stdout)
			return probe.Take(ctx).Write(stdout, width)
		}
		return watch(ctx, probe, stdout, time.Duration(inv.opts.interval)*time.Second)
	}
	if name == "repair" {
		return repair(ctx, inv, os.Stdin, stdout, os.Geteuid() == 0)
	}
	runner := host.NewExec(inv.opts.dryRun)
	runner.Out = stdout
	kit := launch.Kit{Run: runner, Cfg: &inv.cfg, PluginsFile: inv.pluginsFile(), Out: stdout, Root: runner.Root, DryRun: inv.opts.dryRun, Sleep: time.Sleep}
	var srv interface {
		Start(ctx context.Context, mode launch.StartMode) error
		Stop(ctx context.Context, warn int) error
		OpenConsole(ctx context.Context) error
		SendCmd(ctx context.Context, line string) error
	}
	if inv.cfg.RunMode == "native" {
		kit.Console = launch.TmuxConsole{Run: runner, User: inv.cfg.RunUser}
		srv = &launch.Native{Kit: kit}
	} else {
		kit.Console = launch.DockerConsole{Socket: launch.DockerSocket(), Container: launch.Container, DryRun: inv.opts.dryRun, Out: stdout}
		srv = &launch.Boxed{Kit: kit, IDs: launch.HostIDs, Runtime: inv.cfg.RunMode}
	}
	switch name {
	case "stop":
		return srv.Stop(ctx, inv.opts.warn)
	case "console":
		return srv.OpenConsole(ctx)
	case "cmd":
		return srv.SendCmd(ctx, inv.opts.line)
	}
	return srv.Start(ctx, launch.StartMode{Debug: inv.opts.debug, Foreground: inv.opts.foreground})
}

// pluginsFile is the approved jar list, beside the config, so a second
// --config gets its own.
func (inv invocation) pluginsFile() string {
	return filepath.Join(filepath.Dir(inv.configPath), "plugins.shcl")
}

// repairSteps gives the checks around the safe settings and plugins. It's a
// var so tests don't look at the host's real accounts and runtimes.
var repairSteps = func(inv invocation) (h *repairpkg.Host, before, after []repairpkg.Check) {
	runner := host.NewExec(false)
	runner.QuietErr = true
	return repairpkg.NewHost(runner, &inv.cfg, inv.configPath), repairpkg.Before, repairpkg.After
}

// repair runs design.md's repair checks that are built so far. Each one prints
// OK, FIXED, or FAILED. --approve-plugins asks on in.
func repair(ctx context.Context, inv invocation, in io.Reader, w io.Writer, root bool) error {
	if !inv.opts.check && !root {
		return errors.New("repair needs root to fix things; run it with sudo, or add --check to only report")
	}
	failed := false
	report := func(state, text string) {
		fmt.Fprintf(w, "%-7s %s\n", state, text)
		failed = failed || state == "FAILED"
	}
	h, before, after := repairSteps(inv)
	repairpkg.Run(ctx, h, before, inv.opts.check, report)
	check := settings.Fix
	if inv.opts.check {
		check = settings.Check
	}
	problems, err := check(&inv.cfg)
	switch {
	case err != nil:
		report("FAILED", "safe settings: "+err.Error())
	case len(problems) == 0:
		report("OK", "safe settings")
	}
	for _, p := range problems {
		if inv.opts.check {
			report("FAILED", p.String())
		} else {
			report("FIXED", p.Fixed())
		}
	}
	for _, warn := range settings.Warnings(inv.cfg.ServerDir) {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	repairPlugins(inv, in, w, report)
	repairpkg.Run(ctx, h, after, inv.opts.check, report)
	if failed {
		return errors.New("not everything is in place")
	}
	return nil
}

// repairPlugins reports jars that aren't approved, and with --approve-plugins
// asks about each one. A refused jar moves to plugins-refused/. Running out of
// answers leaves the rest as they are.
func repairPlugins(inv invocation, in io.Reader, w io.Writer, report func(state, text string)) {
	file := inv.pluginsFile()
	approved, warns, err := plugins.ReadApproved(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		report("FAILED", "plugins: "+err.Error())
		return
	}
	for _, warn := range warns {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	c, err := plugins.CheckJars(inv.cfg.ServerDir, approved, &plugins.Sums{})
	if err != nil {
		report("FAILED", "plugins: "+err.Error())
		return
	}
	if c.OK() {
		report("OK", fmt.Sprintf("plugins: %d %s, all approved", c.Jars, plural(c.Jars, "jar")))
		return
	}
	if !inv.opts.approve {
		for _, name := range c.New {
			report("FAILED", name+" isn't approved; repair --approve-plugins shows it and asks")
		}
		for _, name := range c.Changed {
			report("FAILED", name+" changed since it was approved; repair --approve-plugins shows it and asks")
		}
		return
	}

	jars, err := plugins.Jars(inv.cfg.ServerDir)
	if err != nil {
		report("FAILED", "plugins: "+err.Error())
		return
	}
	asked := make(map[string]bool, len(c.New)+len(c.Changed))
	for _, name := range append(c.New, c.Changed...) {
		asked[name] = true
	}
	answers := bufio.NewScanner(in)
	var yes []plugins.Info
	var left []string
	for _, j := range jars {
		if !asked[j.Name] {
			continue
		}
		if len(left) > 0 {
			left = append(left, j.Name)
			continue
		}
		info, err := plugins.Describe(j)
		if err != nil {
			report("FAILED", j.Name+": "+err.Error())
			continue
		}
		about := info.About
		if about == "" {
			about = "no plugin.yml"
		}
		state := "new"
		if slices.Contains(c.Changed, j.Name) {
			state = "changed"
		}
		fmt.Fprintf(w, "\n%s (%s)\n  sha256: %s\n  size:   %d bytes\n  date:   %s\n  says:   %s\n",
			j.Name, state, info.SHA256, info.Size, info.Mod.Format("2006-01-02 15:04:05"), about)
		answer, ok := ask(answers, w, "Approve it? [y/n] ")
		switch {
		case !ok:
			left = append(left, j.Name)
		case answer:
			yes = append(yes, info)
		default:
			dest, err := plugins.Refuse(inv.cfg.ServerDir, j, time.Now())
			switch {
			case err != nil:
				report("FAILED", j.Name+" isn't approved: "+err.Error())
			default:
				report("FIXED", j.Name+" refused, and moved to "+dest)
			}
		}
	}
	fmt.Fprintln(w)
	if len(yes) > 0 {
		if err := plugins.Approve(file, yes); err != nil {
			report("FAILED", "plugins: can't write the approved list: "+err.Error())
			return
		}
		for _, j := range yes {
			report("FIXED", j.Name+" approved")
		}
	}
	for _, name := range left {
		report("FAILED", name+" isn't approved; no answer")
	}
}

// ask reads y or n, and asks again on anything else. ok is false when the
// answers ran out.
func ask(answers *bufio.Scanner, w io.Writer, prompt string) (yes, ok bool) {
	for {
		fmt.Fprint(w, prompt)
		if !answers.Scan() {
			fmt.Fprintln(w)
			return false, false
		}
		switch strings.ToLower(strings.TrimSpace(answers.Text())) {
		case "y", "yes":
			return true, true
		case "n", "no":
			return false, true
		}
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// watch redraws status until Ctrl-C. On a terminal each redraw replaces the
// last one; elsewhere they follow one another.
func watch(ctx context.Context, probe *status.Probe, w io.Writer, every time.Duration) error {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		width, tty := terminal(w)
		var b bytes.Buffer
		if tty {
			b.WriteString("\x1b[H\x1b[2J")
		}
		fmt.Fprintf(&b, "\nEvery %s, until Ctrl-C. %s\n", every, time.Now().Format("15:04:05"))
		if err := probe.Take(ctx).Write(&b, width); err != nil {
			return err
		}
		if _, err := w.Write(b.Bytes()); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// terminal gives the width of w, and whether it's a terminal at all.
func terminal(w io.Writer) (width int, ok bool) {
	f, isFile := w.(*os.File)
	if !isFile {
		return 0, false
	}
	var ws struct{ rows, cols, x, y uint16 }
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 { // #nosec G103 -- TIOCGWINSZ fills the struct at that address
		return 0, false
	}
	return int(ws.cols), true
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
	case name == "cmd" && cmdFlags.NArg() == 0:
		return usageErr("cmd: give the console command, such as cmd whitelist add Steve")
	case name == "cmd":
		o.line = strings.Join(cmdFlags.Args(), " ")
	case cmdFlags.NArg() > 0:
		return usageErr("%s: unexpected %q", name, cmdFlags.Arg(0))
	case o.debug && o.foreground:
		return usageErr("start: --debug and --foreground don't go together")
	case o.warn < 0:
		return usageErr("stop: --warn takes 0 or more seconds")
	case o.approve && o.check:
		return usageErr("repair: --approve-plugins and --check don't go together")
	case name == "watch" && o.interval < 1:
		return usageErr("watch: --interval takes 1 or more seconds")
	}
	inv.opts = o
	inv.configPath = o.configPath
	if inv.configPath == "" {
		inv.configPath = defaultConfig
	}

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
	operands := ""
	if c.name == "cmd" {
		operands = " <server command>"
	}
	fmt.Fprintf(&b, "\nUsage:\n  tt-minecraftd %s [options]%s\n\n%s.\n\nOptions:\n", c.name, operands, c.summary)
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
