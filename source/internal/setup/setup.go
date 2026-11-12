// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package setup installs the server from scratch, or adopts a server folder,
// per design.md's setup. It reuses repair's fixes where it can.
package setup

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/firewall"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/repair"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/settings"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/tree"
)

// BinPath is where setup puts the tool.
const BinPath = "/usr/local/bin/tt-minecraftd"

// Setup holds one run's options. Accounts and Repair let tests stay off the
// host's real accounts and checks.
type Setup struct {
	Run host.Runner
	// Loaded is the config as the command line loaded it, for an existing
	// install.
	Loaded      *config.Config
	ConfigPath  string
	PluginsFile string
	// Sets are the keys given on the command line, which go in the config.
	Sets       []config.Option
	Adopt      string
	Yes        bool
	DryRun     bool
	AcceptEULA bool
	In         *bufio.Scanner
	Out        io.Writer
	// Exe is the running binary, copied to BinPath when it's elsewhere.
	Exe string
	// Accounts gives the Host the account checks look at.
	Accounts func(c *config.Config) *repair.Host
	// Repair runs the repair command on c, with check for --check.
	Repair func(ctx context.Context, c *config.Config, check bool) error
	Paper  PaperSource
	Geyser GeyserSource
}

// PaperSource is where Paper builds come from: paper.API, or a test's.
type PaperSource interface {
	NewestVersion(ctx context.Context) (string, error)
	NewestStable(ctx context.Context, version string) (paper.Build, error)
	Install(ctx context.Context, serverDir string, b paper.Build) error
}

// GeyserSource is where Geyser and Floodgate come from: geyser.API, or a
// test's.
type GeyserSource interface {
	Latest(ctx context.Context, project string) (geyser.Build, error)
	Install(ctx context.Context, serverDir string, b geyser.Build) error
	Source(project string) string
}

// Do runs setup: repair on an existing install, the install steps otherwise.
func (s *Setup) Do(ctx context.Context) error {
	if _, err := os.Stat(s.ConfigPath); err == nil {
		return s.existing(ctx)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := s.refuseRunning(ctx); err != nil {
		return err
	}
	opts, err := s.options(ctx)
	if err != nil {
		return err
	}
	cfg, warns, err := config.Load("", opts)
	if err != nil {
		return err
	}
	for _, w := range warns {
		fmt.Fprintf(s.Out, "warning: %s\n", w)
	}
	if cfg.OwnerAfterStop == "" {
		return errors.New("can't tell who the operator is; run setup through sudo from your own account, or give --set ownerAfterStop=NAME")
	}
	if err := s.checkTree(cfg); err != nil {
		return err
	}
	jars, err := s.newJars(cfg)
	if err != nil {
		return err
	}
	exe := s.copyTarget()
	// Asked before the plan, so a failed lookup stops setup before it changes
	// anything.
	build, err := s.paperBuild(ctx, cfg)
	if err != nil {
		return err
	}
	bedrock, err := s.bedrockBuilds(ctx, cfg)
	if err != nil {
		return err
	}

	s.plan(cfg, opts, len(jars), exe, build, bedrock)
	if !s.confirm("Go ahead? [y/n] ") {
		return errors.New("nothing changed")
	}
	if err := s.install(ctx, cfg); err != nil {
		return err
	}
	failed := false
	report := s.reporter(&failed)
	stopped := errors.New("setup stopped at the FAILED step above; fix it and run setup again")
	s.accounts(ctx, &cfg, report)
	if failed {
		return stopped
	}
	if err := s.makeTree(ctx, &cfg); err != nil {
		return err
	}
	if err := s.getPaper(ctx, cfg, build); err != nil {
		return err
	}
	if err := s.eula(cfg); err != nil {
		return err
	}
	s.safeSettings(&cfg, report)
	if failed {
		return stopped
	}
	if err := s.getBedrock(ctx, cfg, bedrock); err != nil {
		return err
	}
	if err := s.approve(jars); err != nil {
		return err
	}
	s.firewall(ctx, &cfg, report)
	if failed {
		return stopped
	}
	if cfg.RunMode != "native" {
		if _, err := s.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{cfg.RunMode, "pull", cfg.Image}}); err != nil {
			return err
		}
	}
	if exe != "" {
		if _, err := s.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{"install", "-m", "0755", exe, BinPath}}); err != nil {
			return err
		}
	}
	// Last, so a setup that stops partway runs again as a new install.
	if err := s.writeConfig(opts); err != nil {
		return err
	}
	if s.DryRun {
		report("PLAN", "repair --check runs last")
		return nil
	}
	fmt.Fprintln(s.Out, "\nChecking the install, as repair --check does:")
	return s.Repair(ctx, &cfg, true)
}

// existing runs repair on an install that's already there, after getting a
// Geyser or Floodgate that's missing, since repair only reports those. update
// comes after it once update is built.
func (s *Setup) existing(ctx context.Context) error {
	if s.Adopt != "" {
		return fmt.Errorf("%s is there already, so this isn't a new install; set serverDir in it to move to another server folder", s.ConfigPath)
	}
	cfg := s.Loaded
	var bedrock []geyser.Build
	// A missing tree is repair's to report.
	if _, err := os.Stat(cfg.ServerDir); err == nil {
		if bedrock, err = s.bedrockBuilds(ctx, *cfg); err != nil {
			return err
		}
	}
	prompt := "Run repair? [y/n] "
	if len(bedrock) == 0 {
		fmt.Fprintf(s.Out, "\nFound an install at %s, so setup runs repair on it.\n\n", s.ConfigPath)
	} else {
		fmt.Fprintf(s.Out, "\nFound an install at %s, so setup will:\n", s.ConfigPath)
		s.planBedrock(bedrock)
		fmt.Fprintf(s.Out, "  - Run repair on it.\n\n")
		prompt = "Go ahead? [y/n] "
	}
	if !s.confirm(prompt) {
		return errors.New("nothing changed")
	}
	if err := s.getBedrock(ctx, *cfg, bedrock); err != nil {
		return err
	}
	return s.Repair(ctx, cfg, s.DryRun)
}

func (s *Setup) refuseRunning(ctx context.Context) error {
	ps, err := s.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"ps", "-eo", "user=,pid=,args="}})
	if err != nil {
		return err
	}
	if found := server.RunningPaper(ps); len(found) > 0 {
		return fmt.Errorf("a Paper server is running (%s); stop it before setup", strings.Join(found, "; "))
	}
	return nil
}

// options are the keys the config gets: the ones given, the runtime when none
// was, and serverDir from --adopt.
func (s *Setup) options(ctx context.Context) ([]config.Option, error) {
	opts := append([]config.Option(nil), s.Sets...)
	if s.Adopt != "" {
		dir, err := filepath.Abs(s.Adopt)
		if err != nil {
			return nil, err
		}
		if !plainPath.MatchString(dir) {
			return nil, fmt.Errorf("--adopt %q: setup only takes a path of letters, digits, and ._-/ here; give it as --set serverDir=\"...\" instead", s.Adopt)
		}
		opts = append(opts, config.Option{Key: "serverDir", Value: dir})
	}
	if given(opts, "runMode") {
		return opts, nil
	}
	mode, err := s.pickRuntime(ctx)
	if err != nil {
		return nil, err
	}
	return append(opts, config.Option{Key: "runMode", Value: mode}), nil
}

// hasCatatonit is a var so tests don't depend on the host's.
var hasCatatonit = repair.HasCatatonit

// hasNft is a var for the same reason.
var hasNft = repair.HasNft

var plainPath = regexp.MustCompile(`^/[A-Za-z0-9_./-]*$`)

func given(opts []config.Option, key string) bool {
	for _, o := range opts {
		if strings.EqualFold(o.Key, key) {
			return true
		}
	}
	return false
}

// pickRuntime is design.md's pick: Podman, else Docker, else Podman through
// apt or native mode. Docker is never installed.
func (s *Setup) pickRuntime(context.Context) (string, error) {
	switch {
	case launch.Installed("podman"):
		return "podman", nil
	case launch.Installed("docker"):
		return "docker", nil
	case !launch.Installed("apt-get"):
		return "", errors.New("neither Podman nor Docker is installed; install Podman and catatonit, or give --native")
	}
	fmt.Fprintln(s.Out, "\nNeither Podman nor Docker is installed. Podman runs the server in a container, which keeps it away from the rest of the host. Native mode runs it in a systemd sandbox instead, which is less.")
	if s.confirm("Install Podman and catatonit through apt? n uses native mode [y/n] ") {
		return "podman", nil
	}
	return "native", nil
}

// packages are what install puts in through apt, for the Podman pick.
func (s *Setup) packages(c config.Config) []string {
	var pkgs []string
	if c.RunMode == "podman" && !launch.Installed("podman") {
		pkgs = append(pkgs, "podman")
	}
	if c.RunMode == "podman" && !hasCatatonit() {
		pkgs = append(pkgs, "catatonit")
	}
	// Without apt, the firewall step says what's missing.
	if c.Firewall && !hasNft() && launch.Installed("apt-get") {
		pkgs = append(pkgs, "nftables")
	}
	return pkgs
}

func (s *Setup) install(ctx context.Context, c config.Config) error {
	pkgs := s.packages(c)
	if len(pkgs) == 0 {
		return nil
	}
	_, err := s.Run.Run(ctx, host.Cmd{Sudo: true, Tty: true, Args: append([]string{"apt-get", "install", "-y"}, pkgs...)})
	return err
}

// checkTree refuses a new install over a folder that has files in it, since
// --adopt is how to take one over.
func (s *Setup) checkTree(c config.Config) error {
	entries, err := os.ReadDir(c.ServerDir)
	switch {
	case s.Adopt != "" && err != nil:
		return fmt.Errorf("--adopt: %w", err)
	case s.Adopt != "":
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case len(entries) > 0:
		return fmt.Errorf("%s has files in it already; give --adopt %[1]s to take it over as it is", c.ServerDir)
	}
	return nil
}

// newJars are the plugin jars that aren't approved yet, as on an adopted tree.
func (s *Setup) newJars(c config.Config) ([]plugins.Info, error) {
	if _, err := os.Stat(c.ServerDir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	approved, _, err := plugins.ReadApproved(s.PluginsFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	check, err := plugins.CheckJars(c.ServerDir, approved, &plugins.Sums{})
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(check.New)+len(check.Changed))
	for _, name := range append(check.New, check.Changed...) {
		want[name] = true
	}
	all, err := plugins.Jars(c.ServerDir)
	if err != nil {
		return nil, err
	}
	var jars []plugins.Info
	for _, j := range all {
		if !want[j.Name] {
			continue
		}
		info, err := plugins.Describe(j)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", j.Name, err)
		}
		jars = append(jars, info)
	}
	return jars, nil
}

// copyTarget is the binary to copy to BinPath, or empty when it runs from
// there already.
func (s *Setup) copyTarget() string {
	if s.Exe == "" {
		return ""
	}
	exe, err := filepath.EvalSymlinks(s.Exe)
	if err != nil {
		exe = s.Exe
	}
	if bin, err := filepath.EvalSymlinks(BinPath); err == nil && bin == exe {
		return ""
	}
	return exe
}

func (s *Setup) plan(c config.Config, opts []config.Option, jars int, exe string, build *paper.Build, bedrock []geyser.Build) {
	w := s.Out
	fmt.Fprintf(w, "\nsetup will:\n")
	if pkgs := s.packages(c); len(pkgs) > 0 {
		fmt.Fprintf(w, "  - Install %s through apt.\n", strings.Join(pkgs, " and "))
	}
	fmt.Fprintf(w, "  - Make account %s and group %s where they're missing, and put %s in the group.\n", c.RunUser, c.RunGroup, c.OwnerAfterStop)
	if s.Adopt != "" {
		fmt.Fprintf(w, "  - Take over %s as it is, and hand it to %s, group %s.\n", c.ServerDir, c.OwnerAfterStop, c.RunGroup)
	} else {
		fmt.Fprintf(w, "  - Make %s, owned by %s, group %s.\n", c.ServerDir, c.OwnerAfterStop, c.RunGroup)
	}
	if build != nil {
		fmt.Fprintf(w, "  - Download Paper %s build %d, check its SHA-256, approve it, and point paper.jar at it.\n", build.Version, build.ID)
	}
	if accepted, _ := settings.EULAAccepted(c.ServerDir); !accepted {
		fmt.Fprintf(w, "  - Ask whether you accept the Minecraft EULA, and write eula.txt only after a yes.\n")
	}
	fmt.Fprintf(w, "  - Write the safe server settings.\n")
	s.planBedrock(bedrock)
	if jars > 0 {
		fmt.Fprintf(w, "  - Approve the %d plugin %s there, listed with %s hashes below.\n", jars, plural(jars, "jar"), plural(jars, "its", "their"))
	}
	if c.Firewall {
		fmt.Fprintf(w, "  - Write the firewall rules to %s, and enable and start %s.\n", firewall.RulesFile, firewall.UnitName)
	}
	if c.RunMode != "native" {
		fmt.Fprintf(w, "  - Pull %s with %s.\n", c.Image, c.RunMode)
	}
	if exe != "" {
		fmt.Fprintf(w, "  - Copy %s to %s.\n", exe, BinPath)
	}
	fmt.Fprintf(w, "  - Write %s, with %s.\n", s.ConfigPath, keyList(opts))
	fmt.Fprintf(w, "  - Check it all, as repair --check does.\n\n")
}

func (s *Setup) planBedrock(bedrock []geyser.Build) {
	if len(bedrock) == 0 {
		return
	}
	names := make([]string, len(bedrock))
	for i, b := range bedrock {
		names[i] = fmt.Sprintf("%s %s build %d", geyser.Title(b.Project), b.Version, b.ID)
	}
	fmt.Fprintf(s.Out, "  - Download %s into plugins/, check %s SHA-256, and approve %s.\n", strings.Join(names, " and "), plural(len(bedrock), "its", "their"), plural(len(bedrock), "it", "them"))
}

func keyList(opts []config.Option) string {
	var b strings.Builder
	for i, o := range lastOfEach(opts) {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(o.Key + " " + o.Value)
	}
	return b.String()
}

// lastOfEach keeps the last value given for each key, as Load does, so the
// file doesn't set a key twice.
func lastOfEach(opts []config.Option) []config.Option {
	seen := make(map[string]bool, len(opts))
	var keep []config.Option
	for i := len(opts) - 1; i >= 0; i-- {
		k := strings.ToLower(opts[i].Key)
		if !seen[k] {
			seen[k] = true
			keep = append([]config.Option{opts[i]}, keep...)
		}
	}
	return keep
}

func plural(n int, forms ...string) string {
	if len(forms) == 1 {
		forms = append(forms, forms[0]+"s")
	}
	if n == 1 {
		return forms[0]
	}
	return forms[1]
}

// confirm asks, unless --yes. Under --dry-run nothing changes, so it doesn't.
func (s *Setup) confirm(prompt string) bool {
	if s.Yes || s.DryRun {
		return true
	}
	return s.ask(prompt)
}

// ask reads y or n, and asks again on anything else. No answer is a no.
func (s *Setup) ask(prompt string) bool {
	for {
		fmt.Fprint(s.Out, prompt)
		if !s.In.Scan() {
			fmt.Fprintln(s.Out)
			return false
		}
		switch strings.ToLower(strings.TrimSpace(s.In.Text())) {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

func (s *Setup) reporter(failed *bool) repair.Report {
	return func(state, text string) {
		if s.DryRun && state == "FIXED" {
			state = "PLAN"
		}
		fmt.Fprintf(s.Out, "%-7s %s\n", state, text)
		*failed = *failed || state == "FAILED"
	}
}

// configText is the file setup writes: only the keys that were given or
// picked, so the rest keep following the defaults.
func configText(opts []config.Option) string {
	var b strings.Builder
	b.WriteString("# tt-minecraftd config, written by setup. design.md's Configuration model lists every key.\n")
	for _, o := range lastOfEach(opts) {
		b.WriteString(o.Key + ": " + o.Value + "\n")
	}
	return b.String()
}

// configDir makes the config's folder, which the approved list shares.
func (s *Setup) configDir() (string, error) {
	dir := filepath.Dir(s.ConfigPath)
	return dir, os.MkdirAll(dir, 0o755) // #nosec G301 -- status reads the config without sudo
}

func (s *Setup) writeConfig(opts []config.Option) error {
	text := configText(opts)
	if s.DryRun {
		fmt.Fprintf(s.Out, "would write %s:\n%s", s.ConfigPath, text)
		return nil
	}
	dir, err := s.configDir()
	if err != nil {
		return err
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".config.shcl."+hex.EncodeToString(rnd[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G302 G304 -- readable by all, per design.md's Configuration model
	if err != nil {
		return err
	}
	_, err = f.WriteString(text)
	if err == nil {
		err = f.Chmod(0o644)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, s.ConfigPath)
	}
	if err != nil {
		_ = os.Remove(tmp) // best effort; err is what matters
		return err
	}
	fmt.Fprintf(s.Out, "%-7s %s written\n", "FIXED", s.ConfigPath)
	return nil
}

// firewall is step 9, repair's firewall check with its fixes. A dry run may
// not have nft yet, since apt only printed its install.
func (s *Setup) firewall(ctx context.Context, c *config.Config, report repair.Report) {
	if !c.Firewall {
		return
	}
	if s.DryRun && !hasNft() {
		report("PLAN", "firewall rules written to "+firewall.RulesFile+", and "+firewall.UnitName+" enabled and started")
		return
	}
	h := s.Accounts(c)
	if h.NftThere == nil {
		h.NftThere = hasNft
	}
	repair.Run(ctx, h, []repair.Check{repair.FirewallCheck}, false, report)
}

// paperBuild is the build to download, or nil when there's a paper.jar
// already, as on an adopted tree.
func (s *Setup) paperBuild(ctx context.Context, c config.Config) (*paper.Build, error) {
	if _, err := os.Lstat(filepath.Join(c.ServerDir, "paper.jar")); !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	version, err := s.Paper.NewestVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't get Paper: %w", err)
	}
	b, err := s.Paper.NewestStable(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("can't get Paper: %w", err)
	}
	return &b, nil
}

func (s *Setup) getPaper(ctx context.Context, c config.Config, b *paper.Build) error {
	switch {
	case b == nil:
		return nil
	case s.DryRun:
		fmt.Fprintf(s.Out, "would download %s into %s, and point paper.jar at it\n", b.Name, c.ServerDir)
		return nil
	}
	if err := s.Paper.Install(ctx, c.ServerDir, *b); err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "%-7s Paper %s build %d downloaded, and its SHA-256 matches; paper.jar points at %s\n", "FIXED", b.Version, b.ID, b.Name)
	// start pins the build paper.jar points at, like a plugin.
	if _, err := s.configDir(); err != nil {
		return err
	}
	if err := plugins.Approve(s.PluginsFile, []plugins.Info{{Jar: plugins.Jar{Name: b.Name}, SHA256: b.SHA256, Size: b.Size}}); err != nil {
		return fmt.Errorf("can't write the approved list: %w", err)
	}
	fmt.Fprintf(s.Out, "%-7s %s approved\n", "FIXED", b.Name)
	return nil
}

// bedrockBuilds are the Geyser and Floodgate builds to download with bedrock
// on. A tree that has one already keeps it, as with paper.jar, and setup
// approves it with the other jars.
func (s *Setup) bedrockBuilds(ctx context.Context, c config.Config) ([]geyser.Build, error) {
	if !c.Bedrock {
		return nil, nil
	}
	has, err := geyser.Installed(c.ServerDir)
	if err != nil {
		return nil, err
	}
	var builds []geyser.Build
	for _, p := range geyser.Projects {
		if has[p] {
			continue
		}
		b, err := s.Geyser.Latest(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("can't get %s: %w; or give --set bedrock=false", geyser.Title(p), err)
		}
		builds = append(builds, b)
	}
	return builds, nil
}

// getBedrock downloads Geyser and Floodgate, and approves them with their
// source, for update --plugins. Their configs are left to the plugins' own
// first start, and the safe settings check Geyser's after it.
func (s *Setup) getBedrock(ctx context.Context, c config.Config, builds []geyser.Build) error {
	if len(builds) == 0 {
		return nil
	}
	if s.DryRun {
		for _, b := range builds {
			fmt.Fprintf(s.Out, "would download %s into %s\n", b.Name, filepath.Join(c.ServerDir, "plugins"))
		}
		return nil
	}
	jars := make([]plugins.Info, 0, len(builds))
	for _, b := range builds {
		if err := s.Geyser.Install(ctx, c.ServerDir, b); err != nil {
			return err
		}
		fmt.Fprintf(s.Out, "%-7s %s %s build %d downloaded, and its SHA-256 matches\n", "FIXED", geyser.Title(b.Project), b.Version, b.ID)
		jars = append(jars, plugins.Info{Jar: plugins.Jar{Name: b.Path()}, SHA256: b.SHA256, Source: s.Geyser.Source(b.Project)})
	}
	if _, err := s.configDir(); err != nil {
		return err
	}
	if err := plugins.Approve(s.PluginsFile, jars); err != nil {
		return fmt.Errorf("can't write the approved list: %w", err)
	}
	for _, j := range jars {
		fmt.Fprintf(s.Out, "%-7s %s approved\n", "FIXED", j.Name)
	}
	return nil
}

// accounts makes the account and group, and puts the operator in it. Under
// dry run the group isn't made, so the operator's step is only shown.
func (s *Setup) accounts(ctx context.Context, c *config.Config, report repair.Report) {
	h := s.Accounts(c)
	if !s.DryRun {
		repair.Run(ctx, h, []repair.Check{repair.AccountCheck, repair.OperatorCheck}, false, report)
		return
	}
	repair.Run(ctx, h, []repair.Check{repair.AccountCheck}, false, report)
	if _, err := s.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{"gpasswd", "--add", c.OwnerAfterStop, c.RunGroup}}); err == nil {
		report("PLAN", c.OwnerAfterStop+" added to group "+c.RunGroup)
	}
}

// makeTree makes a new server tree, or hands an adopted one to the operator,
// which is how a stopped server's tree is. See design.md's Tree ownership.
func (s *Setup) makeTree(ctx context.Context, c *config.Config) error {
	if s.Adopt == "" {
		_, err := s.Run.Run(ctx, host.Cmd{Sudo: true, Args: []string{"install", "-d", "-o", c.OwnerAfterStop, "-g", c.RunGroup, "-m", "0770", c.ServerDir}})
		return err
	}
	warns, err := tree.ToOperator(ctx, s.Run, c)
	for _, w := range warns {
		fmt.Fprintf(s.Out, "warning: %s\n", w)
	}
	return err
}

func (s *Setup) eula(c config.Config) error {
	if s.DryRun {
		return nil
	}
	accepted, err := settings.EULAAccepted(c.ServerDir)
	if err != nil || accepted {
		return err
	}
	if !s.AcceptEULA {
		fmt.Fprintf(s.Out, "\nPaper won't start until the Minecraft EULA is accepted. It's at %s.\n", settings.EULAURL)
		if !s.ask("Do you accept the Minecraft EULA? [y/n] ") {
			fmt.Fprintf(s.Out, "eula.txt isn't written, so the server won't start. Run setup again with --accept-eula, or set eula=true in %s yourself.\n", filepath.Join(c.ServerDir, "eula.txt"))
			return nil
		}
	}
	if err := settings.AcceptEULA(c.ServerDir); err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "%-7s eula.txt: eula is now true\n", "FIXED")
	return nil
}

func (s *Setup) safeSettings(c *config.Config, report repair.Report) {
	if s.DryRun {
		report("PLAN", "safe settings get written")
		return
	}
	problems, err := settings.Fix(c)
	if err != nil {
		report("FAILED", "safe settings: "+err.Error())
		return
	}
	if len(problems) == 0 {
		report("OK", "safe settings")
	}
	for _, p := range problems {
		report("FIXED", p.Fixed())
	}
}

func (s *Setup) approve(jars []plugins.Info) error {
	if len(jars) == 0 {
		return nil
	}
	fmt.Fprintln(s.Out)
	for _, j := range jars {
		about := j.About
		if about == "" {
			about = "no plugin.yml"
		}
		fmt.Fprintf(s.Out, "%s\n  sha256: %s\n  says:   %s\n", j.Name, j.SHA256, about)
	}
	if s.DryRun {
		return nil
	}
	if _, err := s.configDir(); err != nil {
		return err
	}
	if err := plugins.Approve(s.PluginsFile, jars); err != nil {
		return fmt.Errorf("can't write the approved list: %w", err)
	}
	fmt.Fprintf(s.Out, "%-7s %d plugin %s approved\n", "FIXED", len(jars), plural(len(jars), "jar"))
	return nil
}
