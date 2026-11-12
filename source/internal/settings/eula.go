// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"strings"
	"syscall"
)

const eulaFile = "eula.txt"

// EULAURL is the Minecraft EULA that eula.txt agrees to.
const EULAURL = "https://aka.ms/MinecraftEULA"

// EULAAccepted tells whether eula.txt says eula=true, as Paper reads it. A
// missing file is false.
func EULAAccepted(serverDir string) (bool, error) {
	top, err := openDir(serverDir)
	if err != nil {
		return false, err
	}
	defer top.close()
	text, _, err := top.read(eulaFile)
	if isNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	v, _ := parseProperties(text).get("eula")
	return strings.EqualFold(strings.TrimSpace(v), "true"), nil
}

// AcceptEULA sets eula=true in eula.txt, keeping the rest of the file.
func AcceptEULA(serverDir string) error {
	top, err := openDir(serverDir)
	if err != nil {
		return err
	}
	defer top.close()
	text, st, err := top.read(eulaFile)
	var like *syscall.Stat_t
	switch {
	case isNotExist(err):
		text = []byte("#By changing the setting below to TRUE you are indicating your agreement to our EULA (" + EULAURL + ").\n")
	case err != nil:
		return err
	default:
		like = st
	}
	props := parseProperties(text)
	props.set("eula", "true")
	return top.write(eulaFile, props.bytes(), like)
}
