// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/geyser"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/launch"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/linksafe"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/plugins"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/tree"
)

// configCheck runs after the config parsed, since repair doesn't start
// without that.
var configCheck = Check{
	Name: "config",
	Look: func(_ context.Context, h *Host) ([]Finding, error) {
		fi, err := os.Stat(h.ConfigPath)
		if errors.Is(err, fs.ErrNotExist) {
			return []Finding{{Problem: "no config at " + h.ConfigPath + "; sudo tt-minecraftd setup writes one"}}, nil
		}
		if err != nil {
			return nil, err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, errors.New("can't read its owner")
		}
		var found []Finding
		if st.Uid != 0 {
			found = append(found, Finding{
				Problem: fmt.Sprintf("%s is owned by uid %d, so more than root can change it", h.ConfigPath, st.Uid),
				Fixed:   h.ConfigPath + " is now owned by root",
				Fix:     h.fix("chown", "root:root", "--", h.ConfigPath),
			})
		}
		if fi.Mode().Perm()&0o022 != 0 {
			found = append(found, Finding{
				Problem: fmt.Sprintf("%s is mode %04o, so more than root can change it", h.ConfigPath, fi.Mode().Perm()),
				Fixed:   h.ConfigPath + " can only be changed by root now",
				Fix:     h.fix("chmod", "go-w", "--", h.ConfigPath),
			})
		}
		return found, nil
	},
}

var serverTreeCheck = Check{
	Name: "server folder and paper.jar",
	Look: func(_ context.Context, h *Host) ([]Finding, error) {
		dir := h.Cfg.ServerDir
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			// With a config there, setup only runs repair again.
			if _, err := os.Stat(h.ConfigPath); err == nil {
				return []Finding{{Problem: "server folder " + dir + " is missing; set serverDir in " + h.ConfigPath + " to the server folder, or move " + h.ConfigPath + " aside and run sudo tt-minecraftd setup again, which makes a new one or adopts one with --adopt"}}, nil
			}
			return []Finding{{Problem: "server folder " + dir + " is missing; sudo tt-minecraftd setup makes one, or adopts one with --adopt"}}, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "paper.jar")); err != nil {
			return []Finding{{Problem: "no paper.jar in " + dir + ", or it links to a build that's gone; sudo tt-minecraftd update --paper gets one"}}, nil
		}
		return nil, nil
	},
}

// ownershipCheck walks the trees and compares them with what start or stop
// leaves. See design.md, Tree ownership.
var ownershipCheck = Check{
	Name: "tree ownership",
	Look: func(ctx context.Context, h *Host) ([]Finding, error) {
		c := h.Cfg
		up, err := launch.Running(ctx, h.Run, c)
		if err != nil {
			return nil, fmt.Errorf("can't tell whether the server runs: %w", err)
		}
		a, err := h.accounts()
		if err != nil {
			return nil, err
		}
		owner := c.OwnerAfterStop
		if up || owner == "" {
			owner = c.RunUser
		}
		u, ok := a.user(owner)
		if !ok {
			return nil, fmt.Errorf("account %s doesn't exist", owner)
		}
		g, ok := a.group(c.RunGroup)
		if !ok {
			return nil, fmt.Errorf("group %s doesn't exist", c.RunGroup)
		}
		// Native mode gives the jars to root while it runs.
		skip := map[string]bool{}
		if up && c.RunMode == "native" {
			jars, _ := launch.LockedJars(c.ServerDir) // missing ones are the server folder check's to report
			for _, j := range jars {
				skip[j] = true
			}
		}

		var out []Finding
		var wrong []string
		for _, t := range c.OwnerTrees {
			real, err := filepath.EvalSymlinks(t)
			if err != nil {
				out = append(out, Finding{Problem: "tree " + t + " is missing"})
				continue
			}
			owners, modes, err := walkTree(real, uint32(u.uid), uint32(g.gid), up, skip)
			if err != nil {
				return nil, err
			}
			if owners > 0 {
				wrong = append(wrong, fmt.Sprintf("%s has %d %s not owned by %s:%s", t, owners, plural(owners, "file"), owner, c.RunGroup))
			}
			if modes > 0 {
				wrong = append(wrong, fmt.Sprintf("%s has %d %s other accounts can reach", t, modes, plural(modes, "file")))
			}
		}
		if len(wrong) == 0 {
			return out, nil
		}
		state := "stopped"
		if up {
			state = "running"
		}
		hand := func(ctx context.Context) error {
			var err error
			if up || c.OwnerAfterStop == "" {
				_, err = tree.ToServer(ctx, h.Run, c)
			} else {
				_, err = tree.ToOperator(ctx, h.Run, c)
			}
			return err
		}
		return append(out, Finding{
			Problem: "with the server " + state + ", " + strings.Join(wrong, "; "),
			Fixed:   "trees handed to " + owner + ":" + c.RunGroup,
			Fix:     hand,
		}), nil
	},
}

// walkTree counts files with the wrong owner, and while the server runs, the
// ones other accounts can reach. A mode tighter than ug+rwX passes, since Java
// makes its temp files 0600 whatever the umask, and Paper saves through them.
// Links aren't followed or counted.
func walkTree(root string, uid, gid uint32, up bool, skip map[string]bool) (owners, modes int, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 || skip[path] {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		if st.Uid != uid || st.Gid != gid {
			owners++
		}
		perm := fi.Mode().Perm()
		if up && perm&0o007 != 0 {
			modes++
		}
		return nil
	})
	return owners, modes, err
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// bedrockCheck covers what the safe settings don't: that the jars are there,
// and the Floodgate key is kept from other accounts. Geyser's config.yml is
// part of the safe settings.
var bedrockCheck = Check{
	Name:    "Geyser and Floodgate",
	Applies: func(c *config.Config) bool { return c.Bedrock },
	Look: func(_ context.Context, h *Host) ([]Finding, error) {
		dir := filepath.Join(h.Cfg.ServerDir, "plugins")
		has, err := geyser.Installed(h.Cfg.ServerDir)
		if err != nil {
			return nil, err
		}
		var found []Finding
		for _, p := range geyser.Projects {
			if !has[p] {
				found = append(found, Finding{Problem: "bedrock is on, but " + geyser.Title(p) + " isn't in " + dir + "; sudo tt-minecraftd setup installs it"})
			}
		}

		key, err := keyFindings(h)
		return append(found, key...), err
	},
}

// FloodgateKeyCheck is the Floodgate key part of bedrockCheck, for setup on an
// adopted tree, where Floodgate has made its key already.
var FloodgateKeyCheck = Check{
	Name:    "Floodgate key",
	Applies: func(c *config.Config) bool { return c.Bedrock },
	Look:    func(_ context.Context, h *Host) ([]Finding, error) { return keyFindings(h) },
}

func keyFindings(h *Host) ([]Finding, error) {
	var found []Finding
	key := filepath.Join(h.Cfg.ServerDir, "plugins", "floodgate", "key.pem")
	f, st, err := floodgateKey(h.Cfg.ServerDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Floodgate makes it at its first start, under the server's umask.
	case errors.Is(err, linksafe.ErrLink):
		found = append(found, Finding{Problem: err.Error() + ", so the Floodgate key can't be checked"})
	case err != nil:
		return nil, err
	case st.Mode&syscall.S_IFMT != syscall.S_IFREG:
		found = append(found, Finding{Problem: key + " isn't a plain file; Floodgate makes a new one when it's moved away"})
	case st.Mode&0o007 != 0:
		found = append(found, Finding{
			Problem: fmt.Sprintf("%s is mode %04o, so other accounts can read the Floodgate key", key, st.Mode&0o777),
			Fixed:   key + " can only be read by its owner and group now",
			Fix:     func(context.Context) error { return closeKey(h.Cfg.ServerDir) },
		})
	}
	if f != nil {
		_ = f.Close() // only read from
	}
	return found, nil
}

// floodgateKey opens plugins/floodgate/key.pem without following a link on
// the way, since the server account could swap one in, and root changes its
// mode.
func floodgateKey(serverDir string) (*os.File, *syscall.Stat_t, error) {
	top, err := linksafe.Open(serverDir)
	if err != nil {
		return nil, nil, err
	}
	defer top.Close()
	d, err := top.Walk("plugins/floodgate")
	if err != nil {
		return nil, nil, err
	}
	defer d.Close()
	return d.OpenFile("key.pem")
}

// closeKey takes away other accounts' access to the key, through the handle
// it checked.
func closeKey(serverDir string) error {
	f, st, err := floodgateKey(serverDir)
	if err != nil {
		return err
	}
	defer f.Close()
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return fmt.Errorf("%s isn't a plain file", f.Name())
	}
	if err := syscall.Fchmod(int(f.Fd()), st.Mode&0o7770); err != nil {
		return &fs.PathError{Op: "chmod", Path: f.Name(), Err: err}
	}
	return nil
}

// GeyserSources are the Geyser and Floodgate jars in plugins/ that file gives
// no source, as on an adopted tree, each with source's for its project.
func GeyserSources(serverDir, file string, source func(project string) string) ([]plugins.Info, error) {
	all, err := plugins.Unsourced(serverDir, file)
	if err != nil {
		return nil, err
	}
	var jars []plugins.Info
	for _, info := range all {
		if p := geyser.Project(info.Name, info.About); p != "" {
			info.Source = source(p)
			jars = append(jars, info)
		}
	}
	return jars, nil
}
