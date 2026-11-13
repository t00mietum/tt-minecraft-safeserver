// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
)

// Player is one entry in whitelist.json or ops.json.
type Player struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// ReadPlayers reads whitelist.json or ops.json from the server tree.
func ReadPlayers(serverDir, file string) ([]Player, error) {
	top, err := openDir(serverDir)
	if err != nil {
		return nil, err
	}
	defer top.close()
	b, _, err := top.read(file)
	if err != nil {
		return nil, err
	}
	var list []Player
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return list, nil
}

// maxOps is where start starts to warn. Every op can change anything.
const maxOps = 2

// Warnings are what's allowed but likely a mistake: an empty whitelist, which
// locks everyone out, too many ops, or an op who can't join.
func Warnings(serverDir string) []string {
	var warns []string
	white, err := ReadPlayers(serverDir, "whitelist.json")
	switch {
	case err != nil && !isNotExist(err):
		warns = append(warns, "can't read the whitelist: "+err.Error())
	case len(white) == 0:
		warns = append(warns, "the whitelist is empty, so nobody can join; add players with tt-minecraftd cmd whitelist add NAME, or fwhitelist add GAMERTAG for Bedrock")
	}
	ops, err := ReadPlayers(serverDir, "ops.json")
	if err != nil {
		if !isNotExist(err) {
			warns = append(warns, "can't read the ops: "+err.Error())
		}
		return warns
	}
	if len(ops) > maxOps {
		names := make([]string, 0, len(ops))
		for _, o := range ops {
			names = append(names, server.Clean(o.Name))
		}
		warns = append(warns, fmt.Sprintf("there are %d ops (%s); each one can change anything, so keep it to %d or fewer", len(ops), strings.Join(names, ", "), maxOps))
	}
	for _, o := range ops {
		if len(white) > 0 && !listed(white, o) {
			warns = append(warns, fmt.Sprintf("op %s isn't on the whitelist", server.Clean(o.Name)))
		}
	}
	return warns
}

func listed(list []Player, p Player) bool {
	for _, w := range list {
		if p.UUID != "" && strings.EqualFold(w.UUID, p.UUID) {
			return true
		}
		if p.UUID == "" && strings.EqualFold(w.Name, p.Name) {
			return true
		}
	}
	return false
}
