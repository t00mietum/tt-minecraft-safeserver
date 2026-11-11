// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package repair

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
)

type account struct {
	name     string
	uid, gid int
	shell    string
}

type group struct {
	name    string
	gid     int
	members []string
}

// accounts reads passwd and group files fresh, since a fix just before may
// have changed them.
type accounts struct {
	users  []account
	groups []group
}

func (h *Host) accounts() (accounts, error) {
	var a accounts
	err := eachLine(h.Passwd, 7, func(f []string) {
		uid, err1 := strconv.Atoi(f[2])
		gid, err2 := strconv.Atoi(f[3])
		if err1 == nil && err2 == nil {
			a.users = append(a.users, account{f[0], uid, gid, f[6]})
		}
	})
	if err != nil {
		return a, err
	}
	err = eachLine(h.Group, 4, func(f []string) {
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			return
		}
		var members []string
		if f[3] != "" {
			members = strings.Split(f[3], ",")
		}
		a.groups = append(a.groups, group{f[0], gid, members})
	})
	return a, err
}

func eachLine(file string, fields int, use func([]string)) error {
	b, err := os.ReadFile(file) // #nosec G304 -- /etc/passwd or /etc/group, or a test's
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) >= fields && !strings.HasPrefix(f[0], "#") {
			use(f)
		}
	}
	return sc.Err()
}

func (a accounts) user(name string) (account, bool) {
	for _, u := range a.users {
		if u.name == name {
			return u, true
		}
	}
	return account{}, false
}

func (a accounts) group(name string) (group, bool) {
	for _, g := range a.groups {
		if g.name == name {
			return g, true
		}
	}
	return group{}, false
}

func (a accounts) userName(uid int) string {
	for _, u := range a.users {
		if u.uid == uid {
			return u.name
		}
	}
	return strconv.Itoa(uid)
}

func (a accounts) inGroup(u account, g group) bool {
	return u.gid == g.gid || slices.Contains(g.members, u.name)
}

var noLogin = []string{"/usr/sbin/nologin", "/sbin/nologin", "/bin/false", "/usr/bin/false"}

// firstLoginUID is where useradd starts people's accounts on Debian, Fedora,
// and Arch. Below it, other than root, is for services.
const firstLoginUID = 1000

var accountCheck = Check{
	Name: "service account",
	Look: func(_ context.Context, h *Host) ([]Finding, error) {
		a, err := h.accounts()
		if err != nil {
			return nil, err
		}
		user, groupName := h.Cfg.RunUser, h.Cfg.RunGroup
		var found []Finding
		g, hasGroup := a.group(groupName)
		if !hasGroup {
			found = append(found, Finding{
				Problem: "group " + groupName + " doesn't exist",
				Fixed:   "group " + groupName + " made",
				Fix:     h.fix("groupadd", "--system", groupName),
			})
		}
		u, ok := a.user(user)
		if !ok {
			return append(found, Finding{
				Problem: "service account " + user + " doesn't exist",
				Fixed:   "service account " + user + " made, in group " + groupName + ", with no login",
				Fix:     h.fix("useradd", "--system", "--gid", groupName, "--no-create-home", "--home-dir", "/nonexistent", "--shell", noLogin[0], user),
			}), nil
		}
		if u.uid == 0 || u.uid >= firstLoginUID {
			// Stripping a person's groups and login would lock them out.
			return append(found, Finding{Problem: fmt.Sprintf("runUser %s is uid %d, which is root or a person's account, so it's left alone; runUser needs an account of its own", user, u.uid)}), nil
		}
		if !slices.Contains(noLogin, u.shell) {
			found = append(found, Finding{
				Problem: fmt.Sprintf("%s can log in, with shell %s", user, u.shell),
				Fixed:   user + "'s shell is now " + noLogin[0],
				Fix:     h.fix("usermod", "--shell", noLogin[0], user),
			})
		}
		if hasGroup && u.gid != g.gid {
			found = append(found, Finding{
				Problem: fmt.Sprintf("%s's own group isn't %s", user, groupName),
				Fixed:   user + "'s own group is now " + groupName,
				Fix:     h.fix("usermod", "--gid", groupName, user),
			})
		}
		for _, other := range a.groups {
			if other.name != groupName && slices.Contains(other.members, user) {
				found = append(found, Finding{
					Problem: user + " is also in group " + other.name,
					Fixed:   user + " taken out of group " + other.name,
					Fix:     h.fix("gpasswd", "--delete", user, other.name),
				})
			}
		}
		return found, nil
	},
}

var operatorCheck = Check{
	Name:    "operator in the server's group",
	Applies: func(c *config.Config) bool { return c.OwnerAfterStop != "" },
	Look: func(_ context.Context, h *Host) ([]Finding, error) {
		a, err := h.accounts()
		if err != nil {
			return nil, err
		}
		op, groupName := h.Cfg.OwnerAfterStop, h.Cfg.RunGroup
		u, ok := a.user(op)
		if !ok {
			return []Finding{{Problem: "operator " + op + ", from ownerAfterStop, doesn't exist"}}, nil
		}
		g, ok := a.group(groupName)
		if !ok {
			return []Finding{{Problem: "operator " + op + " can't be in group " + groupName + ", which doesn't exist"}}, nil
		}
		if a.inGroup(u, g) {
			return nil, nil
		}
		return []Finding{{
			Problem: op + " isn't in group " + groupName + ", so it can't read logs or status while the server runs",
			Fixed:   op + " added to group " + groupName + "; it counts from " + op + "'s next login",
			Fix:     h.fix("gpasswd", "--add", op, groupName),
		}}, nil
	},
}
