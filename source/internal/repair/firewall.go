// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/firewall"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// FirewallCheck is design.md's repair check 9, and setup's step 9.
var FirewallCheck = Check{
	Name: "tt-minecraftd firewall",
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		if !h.Cfg.Firewall {
			return h.firewallOff(ctx)
		}
		return h.firewallOn(ctx)
	},
}

// nftPaths are where Debian and most others put nft. /usr/sbin isn't on a
// plain account's PATH there. A var so tests can give their own.
var nftPaths = []string{"/usr/sbin/nft", "/sbin/nft"}

// HasNft tells whether nftables' nft is installed.
func HasNft() bool { return anyRegular(nftPaths) }

func (h *Host) hasNft() bool {
	if h.NftThere != nil {
		return h.NftThere()
	}
	return HasNft()
}

// fwFiles gives the firewall's files, which tests point elsewhere. An empty
// field left in Files means the real file.
func (h *Host) fwFiles() (f firewall.Files, unit string) {
	f = firewall.Files{Rules: h.RulesFile, ResolvConf: h.ResolvConf, Routes: h.Routes}
	if f.Rules == "" {
		f.Rules = firewall.RulesFile
	}
	unit = h.UnitFile
	if unit == "" {
		unit = firewall.UnitFile
	}
	return f, unit
}

// sameFile says whether path has text. A missing file isn't an error.
func sameFile(path, text string) (bool, error) {
	have, err := os.ReadFile(path) // #nosec G304 -- one of the tool's own paths
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return string(have) == text, err
}

// put writes a root-owned file through the runner, so dry run and tests see it.
func (h *Host) put(ctx context.Context, path, text string) error {
	_, err := h.Run.Run(ctx, host.Cmd{Sudo: true, Stdin: []byte(text), Args: []string{"install", "-D", "-m", "0644", "-o", "root", "-g", "root", "-T", "/dev/stdin", path}})
	return err
}

func (h *Host) firewallOn(ctx context.Context) ([]Finding, error) {
	if !h.hasNft() {
		return []Finding{{Problem: "nftables isn't installed, so the firewall can't load; " + adds("nftables")}}, nil
	}
	files, unitPath := h.fwFiles()
	rulesPath := files.Rules
	local, err := firewall.ReadLocal(files)
	if err != nil {
		return nil, err
	}
	rules, unit := firewall.Rules(h.Cfg, local), firewall.Unit()
	var found []Finding
	stale := false

	written := firewall.ForConfig(rules, h.ConfigPath)
	have, err := os.ReadFile(rulesPath) // #nosec G304 -- one of the tool's own paths
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	switch other := h.rulesOwner(string(have), unitPath); {
	case string(have) == written:
	case other != "" && firewall.StripOwner(string(have)) == rules:
		// The other config's file gives this one's rules too.
	case other != "":
		return []Finding{{Problem: "firewall is on, but " + rulesPath + " is written for the config at " + other + ", with other rules; there's one firewall table, so only one config's rules can load"}}, nil
	default:
		stale = true
		found = append(found, Finding{
			Problem: rulesPath + " is missing, or doesn't match the config",
			Fixed:   rulesPath + " written from the config",
			Fix:     func(ctx context.Context) error { return h.put(ctx, rulesPath, written) },
		})
	}
	ok, err := sameFile(unitPath, unit)
	if err != nil {
		return nil, err
	}
	if !ok {
		stale = true
		found = append(found, Finding{
			Problem: unitPath + " is missing, or out of date",
			Fixed:   unitPath + " written",
			Fix: func(ctx context.Context) error {
				if err := h.put(ctx, unitPath, unit); err != nil {
					return err
				}
				return h.fix("systemctl", "daemon-reload")(ctx)
			},
		})
	}
	found = append(found, h.enabled(ctx, firewall.UnitName, firewall.UnitName+" isn't enabled, so the rules don't load at boot")...)

	reload := Finding{
		Fixed: "firewall rules loaded",
		Fix:   h.fix("systemctl", "reload-or-restart", firewall.UnitName),
	}
	if stale {
		reload.Problem = "the loaded rules are out of date"
		return append(found, reload), nil
	}
	up, err := firewall.Loaded(ctx, h.Run)
	if err != nil {
		return nil, err
	}
	if !up {
		reload.Problem = "the nftables table inet " + firewall.Table + " isn't loaded, so start refuses"
		found = append(found, reload)
	}
	return found, nil
}

// rulesOwner is the config a rules file was written for, when that's another
// one and its unit is still installed. Once that config turns its firewall
// off, the file left behind is anyone's.
func (h *Host) rulesOwner(text, unitPath string) string {
	owner := firewall.Owner(text)
	if owner == "" || boot.SameConfig(owner, h.ConfigPath) {
		return ""
	}
	if _, err := os.Lstat(unitPath); err != nil {
		return ""
	}
	return owner
}

// firewallOff takes the unit and the table away, and leaves the rules file.
func (h *Host) firewallOff(ctx context.Context) ([]Finding, error) {
	files, unitPath := h.fwFiles()
	if text, err := os.ReadFile(files.Rules); err == nil { // #nosec G304 -- one of the tool's own paths
		if other := h.rulesOwner(string(text), unitPath); other != "" {
			return []Finding{{Note: true, Problem: files.Rules + " and " + firewall.UnitName + " are for the config at " + other + ", so they're left alone"}}, nil
		}
	}
	_, err := os.Lstat(unitPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		return []Finding{{
			Problem: "firewall is off, but " + firewall.UnitName + " is still installed",
			Fixed:   firewall.UnitName + " and its table removed",
			// Stopping it deletes the table.
			Fix: h.fixAll(
				[]string{"systemctl", "disable", "--now", firewall.UnitName},
				[]string{"rm", "-f", "--", unitPath},
				[]string{"systemctl", "daemon-reload"},
			),
		}}, nil
	}
	if !h.hasNft() {
		return nil, nil
	}
	up, err := firewall.Loaded(ctx, h.Run)
	if err != nil || !up {
		return nil, err
	}
	return []Finding{{
		Problem: "firewall is off, but the nftables table inet " + firewall.Table + " is still loaded",
		Fixed:   "table inet " + firewall.Table + " removed",
		Fix:     h.fix("nft", "delete", "table", "inet", firewall.Table),
	}}, nil
}
