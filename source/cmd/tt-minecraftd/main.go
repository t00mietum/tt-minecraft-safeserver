// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// tt-minecraftd runs a Paper server locked down, and checks it at every start.
package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	agentpkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/agent"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/oldbuild"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	repairpkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/repair"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
	setuppkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/setup"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/status"
	updatepkg "github.com/t00mietum/tt-minecraft-safeserver/source/internal/update"
)

// version is set at release with -ldflags "-X main.version=...".
var version = "v1.0.0-alpha.1"

// defaultConfig is a var so tests don't read the host's real config.
var defaultConfig = config.DefaultPath

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	// Most whole seconds that fit in a time.Duration.
	maxWatchInterval = math.MaxInt64 / int64(time.Second)
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
	yes        bool
	adopt      string
	acceptEULA bool
	// parts are update's --paper and the like. None named means all.
	parts updatepkg.Parts
	// keepSelf left --self out of setup's update. --self is deferred
	// (backlog 2026111318002865).
	// keepSelf bool
	// line is cmd's console command.
	line string
	// agentAnswer is the file update's operator run left the agent's answer
	// in, for its run through sudo.
	agentAnswer string
	// mcVersion is update's --mc-version.
	mcVersion string
}

type command struct {
	name    string
	summary string
	flags   func(fs *flag.FlagSet, o *options)
}

var commands = []command{
	{"start", "Check the settings and start the server", func(fs *flag.FlagSet, o *options) {
		if config.NativeMode {
			fs.BoolFunc("native", "Run in native mode, the same as --set runMode=native", func(string) error {
				o.sets = append(o.sets, config.Option{Key: "runMode", Value: "native"})
				return nil
			})
		}
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
		fs.BoolVar(&o.check, "dry-run", false, "The same as --check")
	}},
	{"setup", "Install the server, or adopt a server folder", func(fs *flag.FlagSet, o *options) {
		if config.NativeMode {
			fs.BoolFunc("native", "Use native mode, the same as --set runMode=native", func(string) error {
				o.sets = append(o.sets, config.Option{Key: "runMode", Value: "native"})
				return nil
			})
		}
		fs.BoolVar(&o.yes, "yes", false, "Don't ask before changing things. The EULA still needs a yes or --accept-eula")
		fs.BoolVar(&o.dryRun, "dry-run", false, "Print the plan and the commands, and change nothing")
		fs.StringVar(&o.adopt, "adopt", "", "Take over the server folder at `PATH` as it is")
		fs.BoolVar(&o.acceptEULA, "accept-eula", false, "Accept the Minecraft EULA, "+settings.EULAURL)
	}},
	{"update", "Get newer builds. A running server uses them from its next start", func(fs *flag.FlagSet, o *options) {
		// --self is deferred: packages and the install script update the binary (backlog 2026111318002865).
		// fs.BoolVar(&o.parts.Self, "self", false, "Replace tt-minecraftd with its newest release from GitHub. On NixOS, say what to change")
		fs.BoolVar(&o.parts.Paper, "paper", false, "Update Paper to the newest stable build of its Minecraft version")
		fs.BoolVar(&o.parts.Plugins, "plugins", false, "Update the plugins whose source is known, such as Geyser and Floodgate, and ask the update agent about the rest when agent is set")
		fs.BoolVar(&o.parts.Image, "image", false, "Pull the newest build of the image's tag, check its Java, and pin its digest in the config")
		fs.StringVar(&o.mcVersion, "mc-version", "", "Move Paper to Minecraft version `V`. Asks even with --yes, and takes a ZFS snapshot first")
		fs.BoolVar(&o.yes, "yes", false, "Don't ask before changing things")
		fs.BoolVar(&o.dryRun, "dry-run", false, "Print the plan, and change nothing")
		fs.StringVar(&o.agentAnswer, "agent-answer", "", "")
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
	// args are the arguments as given, for setup to run itself through sudo.
	args []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == retireCmd {
		return failed(retireBuild(args[1:], stdout, stderr), stderr)
	}
	inv, code, done := prepare(args, stdout, stderr)
	if done {
		return code
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	return failed(dispatch(ctx, inv, stdout), stderr)
}

// failed shows err and gives the exit code for it.
func failed(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	var code exitCode
	if errors.As(err, &code) {
		return int(code)
	}
	// Errors often name a file the server account named.
	fmt.Fprintf(stderr, "tt-minecraftd: %s\n", server.CleanLines(err.Error()))
	// The boot unit's start --foreground ends with the server's own code, so
	// systemd sees a crash.
	var exit launch.ExitError
	if errors.As(err, &exit) && exit.Code > 0 && exit.Code < 256 {
		return exit.Code
	}
	return exitError
}

func dispatch(ctx context.Context, inv invocation, stdout io.Writer) error {
	name := inv.cmd.name
	if name == "status" || name == "watch" {
		runner := host.NewExec(false)
		runner.QuietErr = true
		probe := status.NewProbe(runner, &inv.cfg)
		if name == "status" {
			return showStatus(ctx, probe, stdout)
		}
		return watch(ctx, probe, stdout, time.Duration(inv.opts.interval)*time.Second)
	}
	if name == "repair" {
		return repair(ctx, inv, stdout, os.Geteuid() == 0)
	}
	if name == "setup" {
		return setup(ctx, inv, os.Stdin, stdout, os.Geteuid() == 0)
	}
	if name == "update" {
		return update(ctx, inv, os.Stdin, stdout, os.Geteuid() == 0)
	}
	if name != "start" {
		follow(ctx, &inv.cfg, stdout)
	}
	runner := host.NewExec(inv.opts.dryRun)
	runner.Out = stdout
	kit := launch.Kit{Run: runner, Cfg: &inv.cfg, Out: stdout, Root: runner.Root, DryRun: inv.opts.dryRun, InUnit: inUnit()}
	if name == "start" || name == "stop" {
		served, err := bootServes(ctx, inv.configPath)
		switch {
		case err != nil:
			return err
		case served && (inv.opts.debug || inv.opts.foreground):
			return fmt.Errorf("the boot unit %s is enabled, so start goes through systemctl, which can't run --debug or --foreground; turn startAtBoot off and run repair first", boot.UnitName)
		}
		kit.Boot = served
	}
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
		// Boxed sets its own console once it knows the runtime.
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

// serverMode is where the tool's server runs, from the process table. A var
// for tests.
var serverMode = func(ctx context.Context, c *config.Config) string {
	runner := host.NewExec(false)
	runner.QuietErr = true
	mode, _ := status.NewProbe(runner, c).Mode(ctx) // can't tell: follow keeps runMode
	return mode
}

// inUnit says whether this process runs inside the boot unit. A var so
// tests don't depend on where they run.
var inUnit = boot.InsideNow

// bootServes says whether the boot unit is enabled for this config, from
// outside the unit. Its own start --foreground and stop do the real work. A
// var so tests don't read the host's units.
var bootServes = func(ctx context.Context, configPath string) (bool, error) {
	if inUnit() {
		return false, nil
	}
	runner := host.NewExec(false)
	runner.QuietErr = true
	return boot.Serves(ctx, runner, "", configPath)
}

// follow points stop, console, and cmd at a server that runs natively when
// runMode names a container, or the other way round, as after start
// --native. Without it, stop says the server isn't running and hands the
// trees back while it runs. Boxed already follows a container to the other runtime.
func follow(ctx context.Context, c *config.Config, w io.Writer) {
	mode := serverMode(ctx, c)
	if mode == "" || (mode == "native") == (c.RunMode == "native") {
		return
	}
	fmt.Fprintf(w, "The server runs in %s mode, though runMode says %s.\n", mode, c.RunMode)
	c.RunMode = mode
}

// pluginsFile is plugins.shcl, the plugins' sources for update, beside the
// config, so a second --config gets its own.
func (inv invocation) pluginsFile() string {
	return filepath.Join(filepath.Dir(inv.configPath), "plugins.shcl")
}

// sudoAndExe finds sudo and this binary, for asRoot and asRootAndWait.
func sudoAndExe() (sudo, exe string, err error) {
	if sudo, err = exec.LookPath("sudo"); err != nil {
		return "", "", fmt.Errorf("this needs root, and there's no sudo: %w", err)
	}
	exe, err = os.Executable()
	return sudo, exe, err
}

// asRoot runs the tool again through sudo with the same arguments, for setup,
// update, and repair. A var so tests don't exec.
var asRoot = func(args []string) error {
	sudo, exe, err := sudoAndExe()
	if err != nil {
		return err
	}
	return syscall.Exec(sudo, append([]string{"sudo", "--", exe}, args...), os.Environ()) // #nosec G204 -- this binary, with its own arguments
}

// hostRunner runs setup's and update's commands. A var so tests don't reach
// the host.
var hostRunner = func(dryRun bool) host.Runner { return host.NewExec(dryRun) }

// setupSteps gives setup its runner and the Host its account checks look at.
// A var for tests.
var setupSteps = func(inv invocation) (host.Runner, func(c *config.Config) *repairpkg.Host) {
	runner := hostRunner(inv.opts.dryRun)
	return runner, func(c *config.Config) *repairpkg.Host {
		return repairpkg.NewHost(runner, c, inv.configPath)
	}
}

// setup needs root, so it runs itself again through sudo, unless it's a dry
// run, which changes nothing.
func setup(ctx context.Context, inv invocation, in io.Reader, w io.Writer, root bool) error {
	if !root && !inv.opts.dryRun {
		return asRoot(inv.args)
	}
	// With no path to it, the copy to /usr/local/bin is left out.
	exe, _ := os.Executable()
	runner, accounts := setupSteps(inv)
	// Tests give stand-ins for these through the Host; empty means the real ones.
	bins := accounts(&inv.cfg)
	s := &setuppkg.Setup{
		Run:         runner,
		Loaded:      &inv.cfg,
		ConfigPath:  inv.configPath,
		PluginsFile: inv.pluginsFile(),
		Sets:        inv.opts.sets,
		Adopt:       inv.opts.adopt,
		Yes:         inv.opts.yes,
		DryRun:      inv.opts.dryRun,
		AcceptEULA:  inv.opts.acceptEULA,
		In:          bufio.NewScanner(in),
		Out:         w,
		Exe:         exe,
		PkgBin:      bins.PkgBin,
		LocalBin:    bins.LocalBin,
		OldBuilds:   bins.OldBuilds,
		Accounts:    accounts,
		Paper:       paperAPI,
		Geyser:      geyserAPI,
		Repair: func(ctx context.Context, c *config.Config, check bool) error {
			again := inv
			again.cfg = *c
			again.opts.check = check
			return repair(ctx, again, w, root)
		},
		// setup asked already.
		Update: func(ctx context.Context, c *config.Config) error {
			again := inv
			again.cfg = *c
			again.opts.yes = true
			// again.opts.keepSelf = true
			return update(ctx, again, in, w, root)
		},
	}
	return s.Do(ctx)
}

// retireCmd is left out of the help. install.bash runs it through sudo,
// from the build it's about to install, so the build it replaces is kept
// the same way setup and repair keep one. It copies rather than moves, so
// install.bash's rename of the new build is the only change to the path,
// and something is always there. See design.md, Boot.
const retireCmd = "retire-build"

// oldBuilds is a var so tests don't reach the real folder.
var oldBuilds = oldbuild.Dir

func retireBuild(args []string, w, errW io.Writer) error {
	if len(args) != 1 || filepath.Base(args[0]) != "tt-minecraftd" || !filepath.IsAbs(args[0]) {
		return errors.New(retireCmd + " takes the full path of one tt-minecraftd build")
	}
	to, warn, err := oldbuild.Keep(args[0], oldBuilds, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Kept a copy of %s as %s.\n", args[0], to)
	if warn != nil {
		fmt.Fprintf(errW, "warning: %s\n", warn)
	}
	return nil
}

var userAgent = "tt-minecraftd/" + version + " (https://github.com/t00mietum/tt-minecraft-safeserver)"

// paperAPI and geyserAPI are vars so tests don't reach the real ones.
var (
	paperAPI  = paper.API{UserAgent: userAgent}
	geyserAPI = geyser.API{UserAgent: userAgent}
	// selfAPI   = selfupdate.API{UserAgent: userAgent} // --self is deferred
)

// // running is the binary to replace, past any link to it, so the link still
// // works after. "" when it can't be found.
// func running() string {
// 	exe, err := os.Executable()
// 	if err != nil {
// 		return ""
// 	}
// 	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
// 		return resolved
// 	}
// 	return exe
// }

// update needs root, so it runs itself again through sudo, unless it's a dry
// run. With agent set, the agent is asked first, as the operator, and its
// answer goes to the root run in a file. See design.md's Update agent.
func update(ctx context.Context, inv invocation, in io.Reader, w io.Writer, root bool) error {
	// --self is deferred (backlog 2026111318002865).
	// exe := running()
	// var self updatepkg.SelfSource = selfAPI
	// if inv.opts.keepSelf {
	// 	self = nil
	// }
	u := &updatepkg.Update{
		Cfg:         &inv.cfg,
		ConfigPath:  inv.configPath,
		PluginsFile: inv.pluginsFile(),
		Parts:       inv.opts.parts,
		Yes:         inv.opts.yes,
		DryRun:      inv.opts.dryRun,
		In:          bufio.NewScanner(in),
		Out:         w,
		Paper:       paperAPI,
		Geyser:      geyserAPI,
		// Self:        self,
		// Exe:         exe,
		// Version:     version,
		// NixOS:       selfupdate.FromNix(exe),
		Run:       hostRunner(inv.opts.dryRun),
		Fetch:     download.Client{UserAgent: userAgent},
		MCVersion: inv.opts.mcVersion,
		Root:      root,
	}
	u.Running = func(ctx context.Context) (bool, error) { return launch.Running(ctx, u.Run, &inv.cfg) }
	// Before sudo asks for a password.
	if err := u.Check(); err != nil {
		return err
	}
	parts := inv.opts.parts
	agentOn := inv.cfg.Agent != "" && inv.cfg.Agent != "none" && (parts == updatepkg.Parts{} || parts.Plugins || parts.Paper)
	switch {
	case inv.opts.agentAnswer != "" && (root || inv.opts.dryRun):
		a, err := updatepkg.ReadAgentAnswer(inv.opts.agentAnswer)
		if err != nil {
			return fmt.Errorf("can't read the update agent's answer: %w", err)
		}
		u.Agent = a.Replay
	case !agentOn || inv.opts.agentAnswer != "":
		// Nothing to ask. An answer file goes on to the root run.
	case root:
		u.AgentNote = "the update agent isn't asked when update starts as root. Run update without sudo, and it asks the agent before it runs itself through sudo"
	default:
		asker, err := agentpkg.New(&inv.cfg)
		if err != nil {
			return err
		}
		u.Agent = updatepkg.Live(asker)
		if !inv.opts.dryRun {
			return askThenRoot(ctx, u, inv.args)
		}
	}
	if !root && !inv.opts.dryRun {
		return asRoot(inv.args)
	}
	return u.Do(ctx)
}

// askThenRoot asks the agent as the operator, then runs update through sudo
// with the answer in a file only the operator can read, and removes it after.
func askThenRoot(ctx context.Context, u *updatepkg.Update, args []string) error {
	a, err := u.AskAgent(ctx)
	if err != nil {
		return err
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "tt-minecraftd-agent-")
	if err != nil {
		return err
	}
	file := filepath.Join(dir, "answer.json")
	defer func() {
		_ = os.Remove(file) // best effort; both are only the operator's
		_ = os.Remove(dir)
	}()
	if err := os.WriteFile(file, b, 0o600); err != nil {
		return err
	}
	return asRootAndWait(append(slices.Clone(args), "--agent-answer="+file))
}

// asRootAndWait is asRoot for when something is left to do after, so sudo
// runs as a child. Ctrl-C reaches both, and run's signal context keeps this
// one waiting. A var so tests don't exec.
var asRootAndWait = func(args []string) error {
	sudo, exe, err := sudoAndExe()
	if err != nil {
		return err
	}
	cmd := exec.Command(sudo, append([]string{"--", exe}, args...)...) // #nosec G204 -- this binary, with its own arguments
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// The root run said what went wrong.
		return exitCode(max(exit.ExitCode(), exitError))
	}
	return err
}

// exitCode is an exit code with nothing more to say.
type exitCode int

func (e exitCode) Error() string { return "exit status " + strconv.Itoa(int(e)) }

// repairSteps gives the checks around the safe settings and plugins. It's a
// var so tests don't look at the host's real accounts and runtimes.
var repairSteps = func(inv invocation) (h *repairpkg.Host, before, after []repairpkg.Check) {
	runner := host.NewExec(false)
	runner.QuietErr = true
	runner.NoPrompt = inv.opts.check
	return repairpkg.NewHost(runner, &inv.cfg, inv.configPath), repairpkg.Before, repairpkg.After
}

// repair runs design.md's repair checks that are built so far. Each one prints
// OK, FIXED, or FAILED. Fixing needs root, so like setup it runs itself again
// through sudo.
func repair(ctx context.Context, inv invocation, w io.Writer, root bool) error {
	if !inv.opts.check && !root {
		return asRoot(inv.args)
	}
	failed := false
	report := func(state, text string) {
		fmt.Fprintf(w, "%-7s %s\n", state, server.CleanLines(text))
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
	repairPlugins(inv, w, report)
	repairpkg.Run(ctx, h, after, inv.opts.check, report)
	if failed {
		return errors.New("not everything is in place")
	}
	return nil
}

// repairPlugins gives Geyser and Floodgate GeyserMC's source when
// plugins.shcl has none for them, so update --plugins keeps them current.
// Nothing else about a jar is checked since 2026111914293176.
func repairPlugins(inv invocation, w io.Writer, report func(state, text string)) {
	file := inv.pluginsFile()
	_, warns, err := plugins.ReadApproved(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		report("FAILED", "plugins: "+err.Error())
		return
	}
	for _, warn := range warns {
		fmt.Fprintf(w, "warning: %s\n", server.Clean(warn.String()))
	}
	jars, err := repairpkg.GeyserSources(inv.cfg.ServerDir, inv.cfg.OwnerTrees, file, geyserAPI.Source)
	if err != nil {
		report("FAILED", "plugins: "+err.Error())
		return
	}
	switch {
	case len(jars) == 0:
		report("OK", "plugin sources")
		return
	case inv.opts.check:
		for _, j := range jars {
			report("FAILED", j.Name+" has no source, so update --plugins skips it; repair gives it "+j.Source)
		}
		return
	}
	if err := plugins.SetSources(file, jars); err != nil {
		report("FAILED", "plugins: can't write "+file+": "+err.Error())
		return
	}
	for _, j := range jars {
		report("FIXED", j.Name+" gets its updates from "+j.Source)
	}
}

// showStatus prints one reading. One cut short by Ctrl-C is all errors, so
// it isn't shown.
func showStatus(ctx context.Context, probe *status.Probe, w io.Writer) error {
	width, _ := terminal(w)
	rep := probe.Take(ctx)
	if ctx.Err() != nil {
		return fmt.Errorf("status: %w before the reading was done", launch.ErrInterrupted)
	}
	return rep.Write(w, width)
}

// watch redraws status until Ctrl-C. On a terminal each redraw replaces the
// last one; elsewhere they follow one another. A reading cut short by Ctrl-C
// isn't drawn, so the last whole one stays on the screen.
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
		rep := probe.Take(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err := rep.Write(&b, width); err != nil {
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
		fmt.Fprintf(stdout, "%s\nCopyright © 2026 t00mietum. GPL v2 or later.\n", currentVersionLine())
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
	case name == "watch" && (o.interval < 1 || int64(o.interval) > maxWatchInterval):
		// The upper bound keeps Duration(n)*Second from overflowing, which makes NewTicker panic.
		return usageErr("watch: --interval takes %d to %d seconds", 1, maxWatchInterval)
	}
	inv.opts = o
	inv.args = args
	inv.configPath = cmp.Or(o.configPath, defaultConfig)

	cfg, warns, err := loadConfig(o, name == "setup", stderr)
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
func loadConfig(o options, setup bool, stderr io.Writer) (config.Config, []config.Warning, error) {
	if o.configPath != "" && !setup {
		return config.Load(o.configPath, o.sets)
	}
	path := cmp.Or(o.configPath, defaultConfig)
	cfg, warns, err := config.Load(path, o.sets)
	if errors.Is(err, fs.ErrNotExist) {
		// setup writes it, and reads it again itself on an existing install.
		if !setup {
			fmt.Fprintf(stderr, "warning: no config at %s, so the defaults are used; setup writes one\n", defaultConfig)
		}
		return config.Load("", o.sets)
	}
	return cfg, warns, err
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
		if f.Name == "h" || f.Usage == "" {
			return
		}
		arg, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(b, "  %-20s %s\n", strings.TrimSpace("--"+f.Name+" "+arg), usage)
	})
}

func writeAbout(w io.Writer) {
	fmt.Fprint(w, "\n", currentVersionLine(), `

tt-minecraftd runs a Paper Minecraft server for friends, locked down. The
server runs in a container, or in a systemd sandbox, as its own account. Every
start checks the safe settings first, and plugin jars are read-only while it
runs.

Copyright © 2026 t00mietum. GPL v2 or later.
https://github.com/t00mietum/tt-minecraft-safeserver

`)
}
