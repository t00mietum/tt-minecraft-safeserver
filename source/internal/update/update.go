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
	"os"
	"path/filepath"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
)

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

// Parts are the ones named on the command line. None named means all.
type Parts struct {
	Paper   bool
	Plugins bool
}

// Update holds what one run of the command needs.
type Update struct {
	Cfg         *config.Config
	PluginsFile string
	Parts       Parts
	Yes         bool
	DryRun      bool
	In          *bufio.Scanner
	Out         io.Writer
	Paper       PaperSource
	Geyser      GeyserSource
}

// paperStep is a Paper build to move to, from what paper.jar links to now,
// which is nil with no paper.jar.
type paperStep struct {
	from *paper.Linked
	to   paper.Build
}

// pluginStep is a newer build of an approved plugin with a known source.
type pluginStep struct {
	jar plugins.Approved
	// old is what the jar on disk says it is.
	old string
	to  geyser.Build
}

// Do looks everything up first, so a failed lookup changes nothing, then shows
// the plan and asks.
func (u *Update) Do(ctx context.Context) error {
	if _, err := os.Stat(u.Cfg.ServerDir); err != nil {
		return fmt.Errorf("no server folder: %w", err)
	}
	parts := u.Parts
	if parts == (Parts{}) {
		parts = Parts{Paper: true, Plugins: true}
	}
	var plan []string
	var ps *paperStep
	var err error
	if parts.Paper {
		if ps, err = u.paperLookup(ctx); err != nil {
			return err
		}
		if ps != nil {
			plan = append(plan, ps.plan())
		}
	}
	var steps []pluginStep
	if parts.Plugins {
		if steps, err = u.pluginLookup(ctx); err != nil {
			return err
		}
		for _, s := range steps {
			plan = append(plan, s.plan())
		}
	}
	if len(plan) == 0 {
		return nil
	}
	fmt.Fprintf(u.Out, "\nupdate will:\n  - %s\n\n", strings.Join(plan, "\n  - "))
	if !u.Yes && !u.DryRun && !u.ask("Go ahead? [y/n] ") {
		return errors.New("nothing changed")
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
	if !u.DryRun {
		fmt.Fprintln(u.Out, "A running server goes on with what it started with, and uses the new builds from its next start.")
	}
	return nil
}

func (u *Update) paperLookup(ctx context.Context) (*paperStep, error) {
	cur, err := paper.Current(u.Cfg.ServerDir)
	if err != nil {
		return nil, err
	}
	var version string
	if cur != nil {
		version = cur.Version
	} else if version, err = u.Paper.NewestVersion(ctx); err != nil {
		return nil, fmt.Errorf("can't get Paper: %w", err)
	}
	b, err := u.Paper.NewestStable(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("can't get Paper: %w", err)
	}
	if cur != nil && !cur.Gone && b.ID <= cur.ID {
		fmt.Fprintf(u.Out, "%-7s Paper %s build %d is the newest stable build\n", "OK", cur.Version, cur.ID)
		return nil, nil
	}
	return &paperStep{from: cur, to: b}, nil
}

func (s paperStep) plan() string {
	how := "check its SHA-256, approve it, and point paper.jar at it"
	switch {
	case s.from == nil:
		return fmt.Sprintf("Download Paper %s build %d, %s.", s.to.Version, s.to.ID, how)
	case s.from.Gone:
		return fmt.Sprintf("Download Paper %s build %d, since paper.jar links to %s, which is gone. Then %s.", s.to.Version, s.to.ID, s.from.Target, how)
	}
	return fmt.Sprintf("Move Paper %s from build %d to build %d: download it, %s. The last 2 builds stay.", s.to.Version, s.from.ID, s.to.ID, how)
}

func (u *Update) getPaper(ctx context.Context, s paperStep) error {
	dir := u.Cfg.ServerDir
	if u.DryRun {
		fmt.Fprintf(u.Out, "would download %s into %s, approve it, and point paper.jar at it\n", s.to.Name, dir)
		return nil
	}
	if err := u.Paper.Install(ctx, dir, s.to); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "%-7s Paper %s build %d downloaded, and its SHA-256 matches; paper.jar points at %s\n", "FIXED", s.to.Version, s.to.ID, s.to.Name)
	// start pins the build paper.jar points at, like a plugin.
	if err := plugins.Approve(u.PluginsFile, []plugins.Info{{Jar: plugins.Jar{Name: s.to.Name}, SHA256: s.to.SHA256, Size: s.to.Size}}); err != nil {
		return fmt.Errorf("can't write the approved list: %w", err)
	}
	fmt.Fprintf(u.Out, "%-7s %s approved\n", "FIXED", s.to.Name)
	keep := []string{s.to.Name}
	if s.from != nil && filepath.Dir(s.from.Target) == "." {
		keep = append(keep, s.from.Target)
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

// pluginLookup finds the approved plugins that have a newer build at their
// source. Only GeyserMC's sources are known so far.
func (u *Update) pluginLookup(ctx context.Context) ([]pluginStep, error) {
	approved, warns, err := plugins.ReadApproved(u.PluginsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, w := range warns {
		fmt.Fprintf(u.Out, "warning: %s\n", w)
	}
	var steps []pluginStep
	for _, a := range approved {
		if a.Source == "" {
			continue
		}
		project := u.geyserProject(a.Source)
		if project == "" {
			fmt.Fprintf(u.Out, "%-7s %s: its source isn't one update knows yet, so it stays as it is\n", "SKIPPED", a.Name)
			continue
		}
		file := filepath.Join(u.Cfg.ServerDir, filepath.FromSlash(a.Name))
		if _, err := os.Lstat(file); errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(u.Out, "%-7s %s: approved, but not in the server folder, so it stays out\n", "SKIPPED", a.Name)
			continue
		}
		b, err := u.Geyser.Latest(ctx, project)
		if err != nil {
			return nil, fmt.Errorf("can't get %s: %w", geyser.Title(project), err)
		}
		what := fmt.Sprintf("%s %s build %d", geyser.Title(project), b.Version, b.ID)
		switch {
		case b.SHA256 == a.SHA256:
			fmt.Fprintf(u.Out, "%-7s %s is the newest build, %s\n", "OK", a.Name, what)
			continue
		case b.Path() != a.Name:
			// It would sit beside the old one, and both would load.
			fmt.Fprintf(u.Out, "%-7s %s: the newest build, %s, is named %s; replace it by hand\n", "SKIPPED", a.Name, what, b.Name)
			continue
		}
		old := "an older build"
		if info, err := plugins.Describe(plugins.Jar{Name: a.Name, File: file}); err == nil && info.About != "" {
			old = info.About
		}
		steps = append(steps, pluginStep{jar: a, old: old, to: b})
	}
	return steps, nil
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
	return fmt.Sprintf("Update %s from %s to %s %s build %d: download it, check its SHA-256, and approve it.", s.jar.Name, s.old, geyser.Title(s.to.Project), s.to.Version, s.to.ID)
}

func (u *Update) getPlugin(ctx context.Context, s pluginStep) error {
	if u.DryRun {
		fmt.Fprintf(u.Out, "would download %s over %s, and approve it\n", s.to.Name, s.jar.Name)
		return nil
	}
	if err := u.Geyser.Install(ctx, u.Cfg.ServerDir, s.to); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "%-7s %s %s build %d downloaded, and its SHA-256 matches\n", "FIXED", geyser.Title(s.to.Project), s.to.Version, s.to.ID)
	info := plugins.Info{Jar: plugins.Jar{Name: s.jar.Name}, SHA256: s.to.SHA256, Source: s.jar.Source}
	if err := plugins.Approve(u.PluginsFile, []plugins.Info{info}); err != nil {
		return fmt.Errorf("can't write the approved list: %w", err)
	}
	fmt.Fprintf(u.Out, "%-7s %s approved\n", "FIXED", s.jar.Name)
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
