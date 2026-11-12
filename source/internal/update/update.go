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
	"os"
	"path/filepath"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/paper"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
)

// PaperSource is where Paper builds come from: paper.API, or a test's.
type PaperSource interface {
	NewestVersion(ctx context.Context) (string, error)
	NewestStable(ctx context.Context, version string) (paper.Build, error)
	Install(ctx context.Context, serverDir string, b paper.Build) error
}

// Update holds what one run of the command needs.
type Update struct {
	Cfg         *config.Config
	PluginsFile string
	Yes         bool
	DryRun      bool
	In          *bufio.Scanner
	Out         io.Writer
	Paper       PaperSource
}

// paperStep is a Paper build to move to, from what paper.jar links to now,
// which is nil with no paper.jar.
type paperStep struct {
	from *paper.Linked
	to   paper.Build
}

// Do looks everything up first, so a failed lookup changes nothing, then shows
// the plan and asks.
func (u *Update) Do(ctx context.Context) error {
	if _, err := os.Stat(u.Cfg.ServerDir); err != nil {
		return fmt.Errorf("no server folder: %w", err)
	}
	step, err := u.paperLookup(ctx)
	if err != nil {
		return err
	}
	if step == nil {
		return nil
	}
	fmt.Fprintf(u.Out, "\nupdate will:\n  - %s\n\n", step.plan())
	if !u.Yes && !u.DryRun && !u.ask("Go ahead? [y/n] ") {
		return errors.New("nothing changed")
	}
	return u.getPaper(ctx, *step)
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
	fmt.Fprintln(u.Out, "A running server goes on with the build it started with, and uses the new one from its next start.")
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
