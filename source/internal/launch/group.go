// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"fmt"
	"os"
	"os/user"
	"slices"
	"strconv"
)

// groupState is whether this login has the server's group, which is how the
// operator reads the tree while the server runs.
type groupState int

const (
	// groupUnknown is a group or account that can't be looked up. repair
	// reports that, and the step that needs it fails on its own.
	groupUnknown groupState = iota
	groupHere
	// groupNextLogin is an account the group file lists, in a login that
	// started before it was added, as right after setup.
	groupNextLogin
	groupMissing
)

// loginGroup looks up this login's place in the group, and the account's
// name. A var for tests.
var loginGroup = func(name string) (groupState, string) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return groupUnknown, ""
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return groupUnknown, ""
	}
	gids, err := os.Getgroups()
	if err != nil {
		return groupUnknown, ""
	}
	if os.Getegid() == gid || slices.Contains(gids, gid) {
		return groupHere, ""
	}
	u, err := user.Current()
	if err != nil {
		return groupUnknown, ""
	}
	ids, err := u.GroupIds()
	if err != nil {
		return groupUnknown, u.Username
	}
	if slices.Contains(ids, g.Gid) {
		return groupNextLogin, u.Username
	}
	return groupMissing, u.Username
}

// groupProblem says why this login can't read what in the server tree, or
// nil when it can.
func (k *Kit) groupProblem(what string) error {
	if k.Root {
		return nil
	}
	group := k.Cfg.RunGroup
	state, name := loginGroup(group)
	switch state {
	case groupNextLogin:
		return fmt.Errorf("this login isn't in group %s yet, so it can't read %s; %s was added to it, which counts from the next login, so log in again", group, what, name)
	case groupMissing:
		return fmt.Errorf("%s isn't in group %s, so it can't read %s; sudo gpasswd --add %s %s adds it, from the next login", name, group, what, name, group)
	}
	return nil
}
