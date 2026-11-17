// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/boot"
)

// BootCheck is design.md's repair check 13, and setup's step 11. It never
// starts or stops the server, only what happens at the next boot.
var BootCheck = Check{
	Name: "boot unit",
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		path := h.BootFile
		if path == "" {
			path = boot.UnitFile
		}
		if !h.Cfg.StartAtBoot {
			return h.bootOff(path)
		}
		return h.bootOn(ctx, path)
	},
}

func (h *Host) bootOn(ctx context.Context, path string) ([]Finding, error) {
	unit := boot.Unit(h.Cfg, h.ConfigPath)
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
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
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
