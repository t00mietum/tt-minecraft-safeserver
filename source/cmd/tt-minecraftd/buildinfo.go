// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"runtime/debug"
	"strconv"
	"time"
)

// Set at build with -ldflags "-X main.buildEpoch=<commit time, unix seconds>
// -X main.release=1". Without them the build reads the commit time Go
// recorded, if there is one.
var (
	buildEpoch = ""
	release    = "0"
)

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// buildNumber is the whole minutes from 2000-01-01 UTC to t, in lowercase
// Crockford base 32.
func buildNumber(t time.Time) string {
	n := t.Unix()/60 - 946684800/60
	if n <= 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 32 {
		b = append([]byte{crockford[n%32]}, b...)
	}
	return string(b)
}

// commitTime is the time of the commit the binary was built from, or false.
func commitTime() (time.Time, bool) {
	if secs, err := strconv.ParseInt(buildEpoch, 10, 64); err == nil && secs > 0 {
		return time.Unix(secs, 0).UTC(), true
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return time.Time{}, false
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.time" {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				return t.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

// versionLine is "tt-minecraftd v<version> build <build>". A build that isn't
// a release adds its date, in the viewer's time zone.
func versionLine(t time.Time, ok bool, isRelease bool) string {
	line := "tt-minecraftd " + version + " build "
	if !ok {
		return line + "unknown"
	}
	line += buildNumber(t)
	if !isRelease {
		line += " (" + t.Local().Format("2006-01-02") + ")"
	}
	return line
}

func currentVersionLine() string {
	t, ok := commitTime()
	return versionLine(t, ok, release == "1")
}
