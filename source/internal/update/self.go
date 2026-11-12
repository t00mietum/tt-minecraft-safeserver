// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"context"
	"errors"
	"fmt"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/selfupdate"
)

// SelfSource is where tt-minecraftd releases come from: selfupdate.API, or a
// test's.
type SelfSource interface {
	Newest(ctx context.Context, current string) (selfupdate.Release, bool, error)
	Install(ctx context.Context, exe string, r selfupdate.Release) error
}

// selfStep is a newer release to swap in for the running binary.
type selfStep struct {
	to selfupdate.Release
}

func (u *Update) selfLookup(ctx context.Context) (*selfStep, error) {
	if u.Self == nil {
		return nil, nil
	}
	r, found, err := u.Self.Newest(ctx, u.Version)
	if err != nil {
		return nil, fmt.Errorf("can't get the newest tt-minecraftd release: %w", err)
	}
	switch {
	case !found:
		fmt.Fprintf(u.Out, "%-7s tt-minecraftd %s is the newest release\n", "OK", u.Version)
		return nil, nil
	case u.NixOS:
		fmt.Fprintf(u.Out, "%-7s tt-minecraftd %s is out, and this is %s. It comes from the Nix store here, so update the flake input it comes from and rebuild, such as with: nix flake update tt-minecraft-safeserver && sudo nixos-rebuild switch\n", "SKIPPED", r.Tag, u.Version)
		return nil, nil
	case u.Exe == "":
		return nil, errors.New("can't find the running tt-minecraftd, to replace it")
	}
	return &selfStep{to: r}, nil
}

func (u *Update) selfPlan(s selfStep) string {
	return fmt.Sprintf("Move tt-minecraftd from %s to %s: download %s, check its SHA-256, and rename it over %s.", u.Version, s.to.Tag, s.to.Asset, u.Exe)
}

func (u *Update) getSelf(ctx context.Context, s selfStep) error {
	if u.DryRun {
		fmt.Fprintf(u.Out, "would download tt-minecraftd %s and rename it over %s\n", s.to.Tag, u.Exe)
		return nil
	}
	if err := u.Self.Install(ctx, u.Exe, s.to); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "%-7s tt-minecraftd %s downloaded, and its SHA-256 matches; it's now %s\n", "FIXED", s.to.Tag, u.Exe)
	return nil
}
