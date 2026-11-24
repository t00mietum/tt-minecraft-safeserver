// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package update gets newer builds of what the server runs, per design.md's
// update. A running server is left alone, and uses them from its next start.
package update

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/agent"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/zfs"
)

// PaperSource is where Paper builds come from: paper.API, or a test's.
type PaperSource interface {
	NewestVersion(ctx context.Context) (string, error)
	NewestStable(ctx context.Context, version string) (paper.Build, error)
	Java(ctx context.Context, version string) (int, error)
	Install(ctx context.Context, serverDir string, b paper.Build) error
}

// GeyserSource is where Geyser and Floodgate come from: geyser.API, or a
// test's.
type GeyserSource interface {
	Latest(ctx context.Context, project string) (geyser.Build, error)
	Install(ctx context.Context, serverDir string, b geyser.Build) error
	Source(project string) string
}

// Parts are the ones named on the command line. None named means all.
type Parts struct {
	Self    bool
	Paper   bool
	Plugins bool
	Image   bool
}

// Update is what one run of the command needs.
type Update struct {
	Cfg         *config.Config
	ConfigPath  string
	PluginsFile string
	Parts       Parts
	Yes         bool
	DryRun      bool
	In          *bufio.Scanner
	Out         io.Writer
	Paper       PaperSource
	Geyser      GeyserSource
	// Self gets tt-minecraftd releases, to replace Exe, the running binary,
	// which is Version. With NixOS, it only says what to change.
	Self    SelfSource
	Exe     string
	Version string
	NixOS   bool
	// Run pulls and tries the image.
	Run host.Runner
	// Agent answers about the jars no known source covers, per design.md's
	// Update agent: Live in the operator's run, or a Replay of its answer in
	// the root one. Nil leaves those jars as they are.
	Agent func(ctx context.Context, q agent.Question) (agent.Answer, error)
	// AgentNote says why Agent is nil though agent is set, such as a run
	// that started as root.
	AgentNote string
	// Fetch gets the agent's jars and hash pages. Its zero value gets from
	// public addresses only.
	Fetch download.Client
	held  *http.Client // Fetch's client with redirects held, made once
	// MCVersion is --mc-version's, the Minecraft version to move Paper to.
	MCVersion string
	// Root skips the zfs try without sudo.
	Root bool
	// Running says the server runs, so its library cache is left alone. Nil
	// is never.
	Running func(ctx context.Context) (bool, error)
	// Now is when the --mc-version snapshot is taken; nil is the clock.
	Now func() time.Time
}

// paperStep is a Paper build to move to, from what paper.jar links to now,
// which is nil with no paper.jar.
type paperStep struct {
	from *paper.Linked
	to   paper.Build
	// move is --mc-version's: to another Minecraft version, or from one
	// that isn't known.
	move bool
	// found is the update agent's, when PaperMC's lookup failed.
	found *agent.PaperBuild
}

// paperFailed is PaperMC's lookup failing for a known Minecraft version,
// which the update agent can cover.
type paperFailed struct {
	version string
	cur     *paper.Linked
	move    bool
	err     error
}

func (e *paperFailed) Error() string { return e.err.Error() }
func (e *paperFailed) Unwrap() error { return e.err }

// pluginStep is a newer build of a plugin with a known source.
type pluginStep struct {
	jar plugins.Approved
	// old is what the jar on disk says it is.
	old string
	to  geyser.Build
}

// Do looks everything up first, so a failed lookup changes nothing, then shows
// the plan and asks.
func (u *Update) Do(ctx context.Context) error {
	if err := u.Check(); err != nil {
		return err
	}
	parts := u.parts()
	if parts != (Parts{Self: true}) {
		if _, err := os.Stat(u.Cfg.ServerDir); err != nil {
			return fmt.Errorf("no server folder: %w", err)
		}
	}
	l, err := u.lookup(ctx, parts, false)
	if err != nil {
		return err
	}
	// One question covers everything the agent is asked, since each can
	// take minutes.
	q := u.fullQuestion(l)
	var ans agent.Answer
	var askErr error
	asked := false
	switch {
	case q.Empty():
	case u.Agent == nil:
		fmt.Fprintf(u.Out, "%-7s %s\n", "NOTE", u.AgentNote)
	default:
		ans, askErr = u.Agent(ctx, q)
		asked = true
		if askErr != nil {
			fmt.Fprintf(u.Out, "%-7s the update agent: %v\n", "FAILED", askErr)
		}
	}
	ok := asked && askErr == nil
	ps := l.ps
	if l.failed != nil {
		if ps, err = u.agentPaper(l.failed, ans, ok); err != nil {
			return err
		}
	}
	var plan []string
	if ps != nil && ps.move && u.Cfg.ZfsDataset != "" {
		plan = append(plan, fmt.Sprintf("Take a ZFS snapshot of %s first.", u.Cfg.ZfsDataset))
	}
	if ps != nil {
		plan = append(plan, ps.plan())
	}
	steps := l.steps
	for _, s := range steps {
		plan = append(plan, s.plan())
	}
	var found []agentStep
	if ok {
		found = u.agentSteps(l.asks, q, ans)
	}
	for _, s := range found {
		plan = append(plan, s.plan(u.DryRun))
	}
	var report []agent.MoveNote
	if ok && q.MoveTo != "" {
		report = agent.CheckMove(ans, q.Plugins)
	}
	if n := len(l.libs); n > 0 {
		plan = append(plan, fmt.Sprintf("Remove the %s plugins asked Paper for, from its cache, so it downloads fresh ones at the next start.", libraries(n, "")))
	}
	var is *imageStep
	if parts.Image {
		if is, err = u.imageLookup(ctx, func() (string, error) { return u.paperVersion(ctx, ps) }); err != nil {
			return err
		}
		if is != nil {
			plan = append(plan, is.plan(u.ConfigPath))
		}
	}
	var ss *selfStep
	if parts.Self {
		if ss, err = u.selfLookup(ctx); err != nil {
			return err
		}
		if ss != nil {
			plan = append(plan, u.selfPlan(*ss))
		}
	}
	if len(plan) == 0 {
		return nil
	}
	// Plan lines name jars from the tree, which the server account names.
	for i := range plan {
		plan[i] = server.Clean(plan[i])
	}
	fmt.Fprintf(u.Out, "\nupdate will:\n  - %s\n\n", strings.Join(plan, "\n  - "))
	if len(report) > 0 {
		showReport(u.Out, q.MoveTo, report)
	}
	move := ps != nil && ps.move
	if move && u.Cfg.ZfsDataset == "" {
		fmt.Fprintf(u.Out, "warning: zfsDataset isn't set, so there's no snapshot to go back to. Back up %s first.\n", u.Cfg.ServerDir)
	}
	switch {
	case u.DryRun:
	case move:
		// --yes doesn't cover it, per design.md's update.
		fmt.Fprintf(u.Out, "Worlds can't go back to an older version once the server starts on Minecraft %s, and plugins may not work on it.\n", ps.to.Version)
		if !u.ask(fmt.Sprintf("Go ahead, and move to %s? [y/n] ", ps.to.Version)) {
			return errors.New("nothing changed")
		}
	case !u.Yes && !u.ask("Go ahead? [y/n] "):
		return errors.New("nothing changed")
	}
	if move {
		if err := u.snapshot(ctx); err != nil {
			return err
		}
	}
	if ps != nil {
		if err := u.getPaper(ctx, *ps); err != nil {
			return err
		}
	}
	for _, s := range steps {
		if err := u.getPlugin(ctx, s); err != nil {
			return err
		}
	}
	for _, s := range found {
		if err := u.getAgentJar(ctx, s); err != nil {
			return err
		}
	}
	// After Paper, so the new build's own libraries are the ones left.
	if err := u.clearLibraries(ctx, l.libs); err != nil {
		return err
	}
	if is != nil {
		if err := u.getImage(ctx, *is); err != nil {
			return err
		}
	}
	// Last, so a failed swap leaves the rest done.
	if ss != nil {
		if err := u.getSelf(ctx, *ss); err != nil {
			return err
		}
	}
	if !u.DryRun && (ps != nil || len(steps) > 0 || len(found) > 0 || is != nil) {
		fmt.Fprintln(u.Out, "A running server goes on with what it started with, and uses the new builds from its next start.")
	}
	return nil
}

// parts are the ones to update, with none named meaning all.
func (u *Update) parts() Parts {
	if u.Parts == (Parts{}) {
		// Self is left out while --self is deferred (backlog 2026111318002865).
		return Parts{Paper: true, Plugins: true, Image: true}
	}
	return u.Parts
}

// agentOn tells whether agent is set, though Agent may be nil in a run that
// started as root.
func (u *Update) agentOn() bool { return u.Agent != nil || u.AgentNote != "" }

// lookups is what's found before the agent is asked anything.
type lookups struct {
	ps *paperStep
	// failed is PaperMC's lookup, for the agent to cover.
	failed *paperFailed
	steps  []pluginStep
	// asks are the jars for the agent, per pluginLookup.
	asks []plugins.Approved
	// libs are the plugins' libraries in Paper's cache, removed so Paper
	// downloads them fresh at the next start.
	libs []plugins.Jar
	// moveTo is --mc-version's, and report the plugins the agent says have
	// a build for it or not.
	moveTo string
	report []plugins.Approved
}

// lookup finds what parts need. The operator's run is quiet, and leaves a
// failed Paper lookup other than PaperMC's to the root run, since it may not
// be able to read the server folder.
func (u *Update) lookup(ctx context.Context, parts Parts, quiet bool) (*lookups, error) {
	var l lookups
	var err error
	if parts.Paper {
		l.ps, err = u.paperLookup(ctx)
		var pf *paperFailed
		switch {
		case errors.As(err, &pf) && u.agentOn():
			l.failed = pf
			if pf.move {
				l.moveTo = pf.version
			}
		case err != nil && !quiet:
			return nil, err
		case l.ps != nil && l.ps.move:
			l.moveTo = l.ps.to.Version
		}
	}
	if parts.Plugins {
		if l.steps, l.asks, err = u.pluginLookup(ctx); err != nil {
			return nil, err
		}
		if l.libs, err = plugins.Libraries(u.Cfg.ServerDir); err != nil && !quiet {
			return nil, fmt.Errorf("can't list Paper's library cache: %w", err)
		}
		// So the plan doesn't offer what clearLibraries would skip.
		if len(l.libs) > 0 && u.mayRun(ctx) {
			l.libs = nil
		}
	}
	if l.moveTo != "" && u.agentOn() {
		l.report = u.movePlugins()
	}
	return &l, nil
}

// movePlugins are the plugins in the tree, for the agent's report before
// --mc-version.
func (u *Update) movePlugins() []plugins.Approved {
	jars, _ := u.treeJars()
	return jars
}

// treeJars are the plugin jars in the tree, each with its source from
// plugins.shcl, if any, and the hash of the file on disk. A jar that can't be
// hashed is left out, with why in skipped. What paper.jar links to is
// --paper's.
func (u *Update) treeJars() (jars []plugins.Approved, skipped []string) {
	found, err := plugins.Jars(u.Cfg.ServerDir)
	if err != nil {
		return nil, []string{err.Error()}
	}
	listed, _, _ := plugins.ReadSources(u.PluginsFile) // only the sources matter here; pluginLookup shows a bad list's warnings
	source := make(map[string]string, len(listed))
	for _, a := range listed {
		source[a.Name] = a.Source
	}
	for _, j := range found {
		if !strings.HasPrefix(j.Name, "plugins/") {
			continue
		}
		// Root shouldn't hash, or tell the agent about, a host file a
		// planted link names.
		if _, err := plugins.Within(j.File, u.Cfg.ServerDir, u.Cfg.OwnerTrees); err != nil {
			skipped = append(skipped, err.Error())
			continue
		}
		sum, err := plugins.Sum(j.File)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: can't read it: %v", j.Name, err))
			continue
		}
		jars = append(jars, plugins.Approved{Name: j.Name, SHA256: sum, Source: source[j.Name]})
	}
	return jars, skipped
}

// fullQuestion is everything the agent is asked in one run.
func (u *Update) fullQuestion(l *lookups) agent.Question {
	q := u.question(l.asks)
	if l.failed != nil {
		q.FindPaper = l.failed.version
	}
	if len(l.report) > 0 {
		q.MoveTo, q.Plugins = l.moveTo, u.question(l.report).Jars
	}
	return q
}

// showReport shows what the agent said about the plugins before a move.
func showReport(w io.Writer, version string, notes []agent.MoveNote) {
	fmt.Fprintf(w, "The update agent's report on the plugins for Minecraft %s. It changes nothing.\n", version)
	for _, n := range notes {
		var what string
		switch n.Has {
		case agent.HasYes:
			what = "has a build"
			if n.Version != "" {
				what += ", " + n.Version
			}
		case agent.HasNo:
			what = "has no build yet"
		default:
			what = "not known"
		}
		if n.Notes != "" {
			what += ". " + n.Notes
		}
		fmt.Fprintf(w, "  - %s: %s\n", server.Clean(n.Jar), what)
	}
	fmt.Fprintln(w)
}

// paperVersion is the Minecraft version the server runs once this update is
// done.
func (u *Update) paperVersion(ctx context.Context, ps *paperStep) (string, error) {
	if ps != nil {
		return ps.to.Version, nil
	}
	cur, err := paper.Current(u.Cfg.ServerDir)
	switch {
	case err != nil:
		return "", err
	case cur != nil:
		return cur.Version, nil
	}
	version, err := u.Paper.NewestVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("can't get Paper: %w", err)
	}
	return version, nil
}

// Check refuses an --mc-version that can't work, before anything runs.
func (u *Update) Check() error {
	switch {
	case u.MCVersion == "":
		return nil
	case !paper.IsVersion(u.MCVersion):
		return fmt.Errorf("--mc-version: %q isn't a Minecraft version", u.MCVersion)
	case u.Parts != (Parts{}) && !u.Parts.Paper:
		return errors.New("--mc-version moves Paper, so it needs --paper, or no part named")
	}
	return nil
}

func (u *Update) paperLookup(ctx context.Context) (*paperStep, error) {
	cur, err := paper.Current(u.Cfg.ServerDir)
	if err != nil {
		return nil, err
	}
	var version string
	switch {
	case u.MCVersion != "":
		version = u.MCVersion
		if cur != nil && paper.Older(version, cur.Version) {
			return nil, fmt.Errorf("paper.jar is on Minecraft %s, and worlds can't go back to %s. To undo a move, roll back the snapshot or backup taken before it", cur.Version, version)
		}
	case cur != nil:
		version = cur.Version
	default:
		if version, err = u.Paper.NewestVersion(ctx); err != nil {
			return nil, fmt.Errorf("can't get Paper: %w", err)
		}
	}
	move := u.MCVersion != "" && (cur == nil || cur.Version != version)
	b, err := u.Paper.NewestStable(ctx, version)
	if err != nil && u.MCVersion != "" {
		return nil, &paperFailed{version, cur, move, fmt.Errorf("can't get Paper for Minecraft %s: %w", version, err)}
	}
	if err != nil {
		return nil, &paperFailed{version, cur, move, fmt.Errorf("can't get Paper: %w", err)}
	}
	if !move && cur != nil && !cur.Gone && b.ID <= cur.ID {
		fmt.Fprintf(u.Out, "%-7s Paper %s build %d is the newest stable build\n", "OK", cur.Version, cur.ID)
		return nil, nil
	}
	return &paperStep{from: cur, to: b, move: move}, nil
}

// snapshot is --mc-version's, of zfsDataset when it's set. A failed one
// stops the move before anything changes.
func (u *Update) snapshot(ctx context.Context) error {
	if u.Cfg.ZfsDataset == "" {
		return nil
	}
	now := time.Now
	if u.Now != nil {
		now = u.Now
	}
	s := zfs.Snapper{Run: u.Run, Dataset: u.Cfg.ZfsDataset, Root: u.Root}
	warns, err := s.Take(ctx, zfs.PreMove, now())
	for _, w := range warns {
		fmt.Fprintf(u.Out, "warning: %s\n", w)
	}
	if err != nil {
		return fmt.Errorf("%w, so nothing changed", err)
	}
	if !u.DryRun {
		fmt.Fprintf(u.Out, "%-7s ZFS snapshot of %s taken\n", "OK", u.Cfg.ZfsDataset)
	}
	return nil
}

func (s paperStep) plan() string {
	how := "check its SHA-256, and point paper.jar at it"
	if f := s.found; f != nil {
		how = fmt.Sprintf("check its SHA-256 %s is on %s, check the download's SHA-256 and that it's Paper for Minecraft %s, ask before using it, then point paper.jar at it. The update agent found it on %s, since PaperMC's lookup failed", f.SHA256, f.HashFrom, s.to.Version, hostOf(f.URL))
		if f.Notes != "" {
			how += ". The agent's notes: " + f.Notes
		}
	}
	switch {
	case s.from == nil:
		return fmt.Sprintf("Download Paper %s build %d, %s.", s.to.Version, s.to.ID, how)
	case s.from.Gone:
		return fmt.Sprintf("Download Paper %s build %d, since paper.jar links to %s, which is gone. Then %s.", s.to.Version, s.to.ID, s.from.Target, how)
	case s.move:
		return fmt.Sprintf("Move Paper from Minecraft %s build %d to Minecraft %s build %d: download it, %s. The last 2 builds stay.", s.from.Version, s.from.ID, s.to.Version, s.to.ID, how)
	}
	return fmt.Sprintf("Move Paper %s from build %d to build %d: download it, %s. The last 2 builds stay.", s.to.Version, s.from.ID, s.to.ID, how)
}

func (u *Update) getPaper(ctx context.Context, s paperStep) error {
	dir := u.Cfg.ServerDir
	switch {
	case u.DryRun && s.found != nil:
		fmt.Fprintf(u.Out, "would check Paper's hash page, download %s into %s, check it, and ask before using it\n", s.to.Name, dir)
		return nil
	case u.DryRun:
		fmt.Fprintf(u.Out, "would download %s into %s, and point paper.jar at it\n", s.to.Name, dir)
		return nil
	case s.found != nil:
		size, use, err := u.getAgentPaper(ctx, s)
		if err != nil || !use {
			return err
		}
		s.to.Size = size
	default:
		if err := u.Paper.Install(ctx, dir, s.to); err != nil {
			return err
		}
	}
	fmt.Fprintf(u.Out, "%-7s Paper %s build %d downloaded, and its SHA-256 matches; paper.jar points at %s\n", "FIXED", s.to.Version, s.to.ID, s.to.Name)
	keep := []string{s.to.Name}
	// Name, not Target: a link written as ./name or a full path never
	// equals a file name, and the build it replaced would go.
	if s.from != nil && s.from.Name != "" {
		keep = append(keep, s.from.Name)
	}
	gone, err := paper.Prune(dir, keep...)
	if len(gone) > 0 {
		fmt.Fprintf(u.Out, "%-7s older builds removed: %s\n", "FIXED", strings.Join(gone, ", "))
	}
	if err != nil {
		return fmt.Errorf("can't remove an older build: %w", err)
	}
	return nil
}

// pluginLookup finds the plugins in the tree that have a newer build at
// their source. Only GeyserMC's sources are known so far. asks are the jars
// for the update agent: ones with no known source, or whose lookup failed.
func (u *Update) pluginLookup(ctx context.Context) (steps []pluginStep, asks []plugins.Approved, err error) {
	_, warns, err := plugins.ReadSources(u.PluginsFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	for _, w := range warns {
		fmt.Fprintf(u.Out, "warning: %s\n", server.Clean(w.String()))
	}
	jars, skipped := u.treeJars()
	for _, why := range skipped {
		fmt.Fprintf(u.Out, "%-7s %s\n", "SKIPPED", server.Clean(why))
	}
	agentOn := u.agentOn()
	for _, a := range jars {
		file := filepath.Join(u.Cfg.ServerDir, filepath.FromSlash(a.Name))
		project := u.geyserProject(a.Source)
		switch {
		case a.Source == "" && !agentOn:
			continue
		case project == "" && !agentOn:
			fmt.Fprintf(u.Out, "%-7s %s: its source isn't one update knows yet, so it stays as it is\n", "SKIPPED", server.Clean(a.Name))
			continue
		case project == "":
			asks = append(asks, a)
			continue
		}
		b, err := u.Geyser.Latest(ctx, project)
		if err != nil && u.Agent != nil {
			fmt.Fprintf(u.Out, "warning: can't get %s: %v, so the update agent is asked about %s\n", geyser.Title(project), err, server.Clean(a.Name))
			asks = append(asks, a)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("can't get %s: %w", geyser.Title(project), err)
		}
		what := fmt.Sprintf("%s %s build %d", geyser.Title(project), b.Version, b.ID)
		switch {
		case b.SHA256 == a.SHA256:
			fmt.Fprintf(u.Out, "%-7s %s is the newest build, %s\n", "OK", server.Clean(a.Name), what)
			continue
		case b.Path() != a.Name:
			// It would sit beside the old one, and both would load.
			fmt.Fprintf(u.Out, "%-7s %s: the newest build, %s, is named %s; replace it by hand\n", "SKIPPED", server.Clean(a.Name), what, b.Name)
			continue
		}
		steps = append(steps, pluginStep{jar: a, old: oldAbout(file), to: b})
	}
	return steps, asks, nil
}

// oldAbout is what the jar on disk says it is.
func oldAbout(file string) string {
	if info, err := plugins.Describe(plugins.Jar{File: file}); err == nil && info.About != "" {
		return info.About
	}
	return "an older build"
}

// geyserProject is the GeyserMC project a source is for, or "".
func (u *Update) geyserProject(source string) string {
	if u.Geyser == nil {
		return ""
	}
	for _, p := range geyser.Projects {
		if source == u.Geyser.Source(p) {
			return p
		}
	}
	return ""
}

func (s pluginStep) plan() string {
	return fmt.Sprintf("Update %s from %s to %s %s build %d: download it, and check its SHA-256.", s.jar.Name, s.old, geyser.Title(s.to.Project), s.to.Version, s.to.ID)
}

func (u *Update) getPlugin(ctx context.Context, s pluginStep) error {
	if u.DryRun {
		fmt.Fprintf(u.Out, "would download %s over %s\n", s.to.Name, s.jar.Name)
		return nil
	}
	if err := u.Geyser.Install(ctx, u.Cfg.ServerDir, s.to); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "%-7s %s %s build %d downloaded, and its SHA-256 matches\n", "FIXED", geyser.Title(s.to.Project), s.to.Version, s.to.ID)
	return nil
}

// ask reads y or n, and asks again on anything else. No answer is a no.
func (u *Update) ask(prompt string) bool {
	for {
		fmt.Fprint(u.Out, prompt)
		if !u.In.Scan() {
			fmt.Fprintln(u.Out)
			return false
		}
		switch strings.ToLower(strings.TrimSpace(u.In.Text())) {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

// clearLibraries removes the plugins' libraries from Paper's cache, as the
// service account, so a link there only leads where it could write anyway.
// Paper downloads them again at the next start, so code written there lasts
// until the next update, not for good. See design.md, Plugin jars.
func (u *Update) clearLibraries(ctx context.Context, libs []plugins.Jar) error {
	// Asked again here, since the "Go ahead?" prompt can wait any length of
	// time, and a server started meanwhile hasn't opened all its libraries.
	if len(libs) == 0 || u.mayRun(ctx) {
		return nil
	}
	args := []string{"rm", "-f", "--"}
	for _, j := range libs {
		args = append(args, filepath.Join(u.Cfg.ServerDir, filepath.FromSlash(j.Name)))
	}
	if _, err := u.Run.Run(ctx, host.Cmd{Args: args, As: u.Cfg.RunUser}); err != nil {
		return fmt.Errorf("clearing Paper's library cache: %w", err)
	}
	if !u.DryRun {
		fmt.Fprintf(u.Out, "%-7s %s removed from Paper's cache; it downloads them again at the next start\n", "FIXED", libraries(len(libs), "plugin "))
	}
	return nil
}

// mayRun says the server may be running, and says so, since a running Paper
// can still open a library jar it hasn't read yet. A failed look counts as
// running.
func (u *Update) mayRun(ctx context.Context) bool {
	if u.Running == nil {
		return false
	}
	if on, err := u.Running(ctx); !on && err == nil {
		return false
	}
	fmt.Fprintf(u.Out, "%-7s plugin libraries: the server may be running, so Paper's cache is left until an update while it's stopped\n", "SKIPPED")
	return true
}

func libraries(n int, kind string) string {
	if n == 1 {
		return "1 " + kind + "library"
	}
	return strconv.Itoa(n) + " " + kind + "libraries"
}
