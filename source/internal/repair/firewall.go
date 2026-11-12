// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

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
func HasNft() bool {
	for _, p := range nftPaths {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func (h *Host) hasNft() bool {
	if h.NftThere != nil {
		return h.NftThere()
	}
	return HasNft()
}

// fwFiles gives the rules file, the unit file, and resolv.conf, which tests
// point elsewhere.
func (h *Host) fwFiles() (rules, unit, resolv string) {
	rules, unit, resolv = h.RulesFile, h.UnitFile, h.ResolvConf
	if rules == "" {
		rules = firewall.RulesFile
	}
	if unit == "" {
		unit = firewall.UnitFile
	}
	if resolv == "" {
		resolv = "/etc/resolv.conf"
	}
	return rules, unit, resolv
}

// sameFile says whether path holds text. A missing file isn't an error.
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
		return []Finding{{Problem: "nftables isn't installed, so the firewall can't load; apt-get install nftables adds it"}}, nil
	}
	rulesPath, unitPath, resolvPath := h.fwFiles()
	conf, err := os.ReadFile(resolvPath) // #nosec G304 -- resolv.conf
	if err != nil {
		return nil, err
	}
	dns, err := firewall.Resolvers(bytes.NewReader(conf))
	if err != nil {
		return nil, err
	}
	rules, unit := firewall.Rules(h.Cfg, dns), firewall.Unit()
	var found []Finding
	stale := false

	ok, err := sameFile(rulesPath, rules)
	if err != nil {
		return nil, err
	}
	if !ok {
		stale = true
		found = append(found, Finding{
			Problem: rulesPath + " is missing, or doesn't match the config",
			Fixed:   rulesPath + " written from the config",
			Fix:     func(ctx context.Context) error { return h.put(ctx, rulesPath, rules) },
		})
	}
	if ok, err = sameFile(unitPath, unit); err != nil {
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
	out, _ := h.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"systemctl", "is-enabled", firewall.UnitName}}) // disabled exits 1
	if strings.TrimSpace(string(out)) != "enabled" {
		found = append(found, Finding{
			Problem: firewall.UnitName + " isn't enabled, so the rules don't load at boot",
			Fixed:   firewall.UnitName + " enabled",
			Fix:     h.fix("systemctl", "enable", firewall.UnitName),
		})
	}

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

// firewallOff takes the unit and the table away, and leaves the rules file.
func (h *Host) firewallOff(ctx context.Context) ([]Finding, error) {
	_, unitPath, _ := h.fwFiles()
	_, err := os.Lstat(unitPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		return []Finding{{
			Problem: "firewall is off, but " + firewall.UnitName + " is still installed",
			Fixed:   firewall.UnitName + " and its table removed",
			Fix: func(ctx context.Context) error {
				// Stopping it deletes the table.
				for _, args := range [][]string{
					{"systemctl", "disable", "--now", firewall.UnitName},
					{"rm", "-f", "--", unitPath},
					{"systemctl", "daemon-reload"},
				} {
					if err := h.fix(args...)(ctx); err != nil {
						return err
					}
				}
				return nil
			},
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
