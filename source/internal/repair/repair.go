// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package repair holds design.md's repair checks. Each one looks, and says what
// it found, with a fix where there is one. setup reuses the fixes.
package repair

import (
	"context"
	"os"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// Host is what the checks look at. Every fix goes through Run.
type Host struct {
	Run        host.Runner
	Cfg        *config.Config
	ConfigPath string
	// Passwd and Group are /etc/passwd and /etc/group, so tests can give
	// their own.
	Passwd, Group string
	// Root is whether repair runs as root, which some reads need.
	Root bool
}

// NewHost gives a Host on the real account files.
func NewHost(r host.Runner, c *config.Config, configPath string) *Host {
	return &Host{Run: r, Cfg: c, ConfigPath: configPath, Passwd: "/etc/passwd", Group: "/etc/group", Root: os.Geteuid() == 0}
}

// Finding is one thing a check found wrong. Fix is nil when repair only
// reports it.
type Finding struct {
	Problem string
	Fixed   string
	Fix     func(ctx context.Context) error
}

// Check is one of design.md's repair checks.
type Check struct {
	Name string
	// Applies, when set, leaves the check out for configs it doesn't fit,
	// such as the container check in native mode.
	Applies func(c *config.Config) bool
	Look    func(ctx context.Context, h *Host) ([]Finding, error)
}

// Report shows one outcome: OK, FIXED, or FAILED.
type Report func(state, text string)

// Run runs each check in turn, so a later one sees what an earlier one fixed.
// With checkOnly, as for --check, nothing is fixed.
func Run(ctx context.Context, h *Host, checks []Check, checkOnly bool, report Report) {
	for _, c := range checks {
		if c.Applies != nil && !c.Applies(h.Cfg) {
			continue
		}
		found, err := c.Look(ctx, h)
		if err != nil {
			report("FAILED", c.Name+": "+err.Error())
			continue
		}
		if len(found) == 0 {
			report("OK", c.Name)
			continue
		}
		for _, f := range found {
			if checkOnly || f.Fix == nil {
				report("FAILED", f.Problem)
				continue
			}
			if err := f.Fix(ctx); err != nil {
				report("FAILED", f.Problem+"; the fix failed: "+err.Error())
				continue
			}
			report("FIXED", f.Fixed)
		}
	}
}

// Before are the checks that come ahead of the safe settings and plugins,
// and After the ones that follow them. See design.md, repair.
var (
	Before = []Check{configCheck, accountCheck, operatorCheck, serverTreeCheck, ownershipCheck}
	After  = []Check{bedrockCheck, hostFirewallCheck, containerCheck, catatonitCheck, datasetCheck, othersCheck}
	// AccountCheck and OperatorCheck are setup's second step.
	AccountCheck, OperatorCheck = accountCheck, operatorCheck
)

// fix runs one command as root.
func (h *Host) fix(args ...string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		_, err := h.Run.Run(ctx, host.Cmd{Args: args, Sudo: true})
		return err
	}
}
