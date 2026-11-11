// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package tree hands the server trees between the operator and the service
// account. See design.md, Tree ownership.
package tree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// ToServer gives each tree to runUser, with modes the group can use. A
// missing tree is a warning.
func ToServer(ctx context.Context, r host.Runner, c *config.Config) ([]string, error) {
	return handOver(ctx, r, c.OwnerTrees, c.RunUser, c.RunGroup, true)
}

// ToOperator gives each tree back to ownerAfterStop. With no ownerAfterStop,
// as when the boot unit stops the server, the trees stay with runUser and
// runGroup, and the next stop by the operator hands them back.
func ToOperator(ctx context.Context, r host.Runner, c *config.Config) ([]string, error) {
	if c.OwnerAfterStop == "" {
		return []string{fmt.Sprintf("ownerAfterStop is empty, so the trees stay with %s; set it, or run stop again as the operator", c.RunUser)}, nil
	}
	return handOver(ctx, r, c.OwnerTrees, c.OwnerAfterStop, c.RunGroup, false)
}

func handOver(ctx context.Context, r host.Runner, trees []string, user, group string, toServer bool) ([]string, error) {
	var warns []string
	for _, t := range trees {
		path, err := realTree(t)
		if errors.Is(err, fs.ErrNotExist) {
			warns = append(warns, fmt.Sprintf("tree %s is missing; skipped", t))
			continue
		}
		if err != nil {
			return warns, err
		}
		if _, err := r.Run(ctx, host.Cmd{Args: []string{"chown", "-R", "--", user + ":" + group, path}, Sudo: true}); err != nil {
			return warns, err
		}
		if !toServer {
			continue
		}
		if _, err := r.Run(ctx, host.Cmd{Args: []string{"chmod", "-R", "ug+rwX,o-rwx", "--", path}, Sudo: true}); err != nil {
			return warns, err
		}
	}
	return warns, nil
}

// realTree follows links, so chown -R changes the tree and not just a link to
// it, and the depth check sees where the link really goes.
func realTree(t string) (string, error) {
	if err := checkDepth(t); err != nil {
		return "", err
	}
	path, err := filepath.EvalSymlinks(t)
	if err != nil {
		return "", err
	}
	if path != t {
		if err := checkDepth(path); err != nil {
			return "", fmt.Errorf("%s links to %s: %w", t, path, err)
		}
	}
	if fi, err := os.Stat(path); err != nil {
		return "", err
	} else if !fi.IsDir() {
		return "", fmt.Errorf("tree %s isn't a folder", t)
	}
	return path, nil
}

// checkDepth refuses / and anything less than 3 levels deep, such as /srv/x,
// since a recursive chown there would take a system folder.
func checkDepth(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("tree %s isn't a full path", path)
	}
	clean := strings.Trim(filepath.Clean(path), "/")
	if clean == "" || strings.Count(clean, "/") < 2 {
		return fmt.Errorf("tree %s is less than 3 levels deep; refused", path)
	}
	return nil
}
