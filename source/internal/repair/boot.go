// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/oldbuild"
)

// BootCheck is design.md's repair check 13, and setup's step 11. It never
// starts or stops the server, only what happens at the next boot.
var BootCheck = Check{
	Name: "boot unit",
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		path := cmp.Or(h.BootFile, boot.UnitFile)
		// There's one boot unit, so one written for another config isn't
		// this config's to change. See design.md, Boot.
		if other := h.bootOwner(path); other != "" {
			if !h.Cfg.StartAtBoot {
				return []Finding{{Note: true, Problem: boot.UnitName + " runs the config at " + other + ", so it's left alone"}}, nil
			}
			return []Finding{{Problem: "startAtBoot is on, but " + boot.UnitName + " runs the config at " + other + "; there's one boot unit, so only one config can start at boot"}}, nil
		}
		if !h.Cfg.StartAtBoot {
			return h.bootOff(path)
		}
		return h.bootOn(ctx, path)
	},
}

// bootOwner is the config the unit at path runs, when that's another one.
// A unit that can't be read or placed counts as this config's.
func (h *Host) bootOwner(path string) string {
	text, err := os.ReadFile(path) // #nosec G304 -- the boot unit's path
	if err != nil {
		return ""
	}
	if owner, ok := boot.Owner(string(text)); ok && !boot.SameConfig(owner, h.ConfigPath) {
		return owner
	}
	return ""
}

func (h *Host) bootOn(ctx context.Context, path string) ([]Finding, error) {
	unit := boot.Unit(h.Cfg, h.ConfigPath, boot.Bin(h.PkgBin))
	var found []Finding
	ok, err := sameFile(path, unit)
	if err != nil {
		return nil, err
	}
	if !ok {
		found = append(found, Finding{
			Problem: "startAtBoot is on, but " + path + " is missing or doesn't match the config",
			Fixed:   path + " written from the config",
			Fix: func(ctx context.Context) error {
				if err := h.put(ctx, path, unit); err != nil {
					return err
				}
				return h.fix("systemctl", "daemon-reload")(ctx)
			},
		})
	}
	return append(found, h.enabled(ctx, boot.UnitName, "startAtBoot is on, but "+boot.UnitName+" isn't enabled")...), nil
}

// bootOff disables and removes the unit, and leaves a server it runs alone.
func (h *Host) bootOff(path string) ([]Finding, error) {
	switch _, err := os.Lstat(path); {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return []Finding{{
		Problem: "startAtBoot is off, but " + boot.UnitName + " is still installed",
		Fixed:   boot.UnitName + " disabled and removed",
		Fix: h.fixAll(
			[]string{"systemctl", "disable", boot.UnitName},
			[]string{"rm", "-f", "--", path},
			[]string{"systemctl", "daemon-reload"},
		),
	}}, nil
}

// shadowCheck is design.md's repair check 15. A hand-installed copy in
// /usr/local/bin comes before the package's /usr/bin one on PATH, so the
// operator keeps running it after a package upgrade. It's moved to the
// old-builds folder rather than removed, since someone may have put that
// build there on purpose.
var shadowCheck = Check{
	Name: "tool on PATH",
	Look: func(_ context.Context, h *Host) ([]Finding, error) {
		pkg, local, kept := cmp.Or(h.PkgBin, boot.PkgBin), cmp.Or(h.LocalBin, boot.LocalBin), cmp.Or(h.OldBuilds, oldbuild.Dir)
		if boot.Bin(pkg) != boot.PkgBin {
			return nil, nil
		}
		lfi, err := os.Stat(local)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// A link to the package's runs the package's.
		if pfi, err := os.Stat(pkg); err == nil && os.SameFile(pfi, lfi) {
			return nil, nil
		}
		problem := boot.LocalBin + " comes before the package's " + boot.PkgBin + " on PATH, so a package upgrade doesn't change what runs"
		to, err := oldbuild.Where(local, kept)
		if err != nil {
			return nil, err
		}
		// A link to some other file has no build of its own to keep.
		if lst, err := os.Lstat(local); err != nil || !lst.Mode().IsRegular() {
			return []Finding{{Problem: problem + "; it's a link, so remove it with: sudo rm " + boot.LocalBin}}, nil
		}
		return []Finding{{
			Problem: problem + "; repair moves it to " + filepath.Join(oldbuild.Dir, filepath.Base(to)),
			Fixed:   boot.LocalBin + " moved to " + oldbuild.Dir + ", so the package's runs",
			Fix: func(context.Context) error {
				_, warn, err := oldbuild.Retire(local, kept, time.Now())
				if err == nil && warn != nil {
					return Warning{warn}
				}
				return err
			},
		}}, nil
	},
}
