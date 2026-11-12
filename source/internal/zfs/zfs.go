// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package zfs takes the start and stop snapshots and prunes old ones. See
// design.md, ZFS snapshots.
package zfs

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// The name after the time, for each kind of snapshot.
const (
	PreStart = "pre_tt-minecraftd-start"
	PostStop = "post_tt-minecraftd-stop"
)

const stampLayout = "20060102-150405"

// ours is the tool's name pattern. Nothing else on the dataset is touched.
var ours = regexp.MustCompile(`^(\d{8}-\d{6})_(` + PreStart + `|` + PostStop + `)$`)

// The schedule: the first, the newest few, and the last of each of the most
// recent days, weeks and months that have one.
const (
	keepNewest = 3
	keepDays   = 7
	keepWeeks  = 4
	keepMonths = 6
)

// Snapper takes snapshots of one dataset.
type Snapper struct {
	Run     host.Runner
	Dataset string
	// Root skips the try without sudo.
	Root bool
}

// Take snapshots the dataset as <time>_<kind>, then prunes. A failed prune
// only warns, since the new snapshot is in.
func (s *Snapper) Take(ctx context.Context, kind string, now time.Time) (warns []string, err error) {
	name := now.Format(stampLayout) + "_" + kind
	if !ours.MatchString(name) {
		return nil, fmt.Errorf("unknown snapshot kind %q", kind)
	}
	if err := s.zfs(ctx, "snapshot", "--", s.Dataset+"@"+name); err != nil {
		return nil, fmt.Errorf("can't snapshot %s: %w", s.Dataset, err)
	}
	if err := s.prune(ctx, name); err != nil {
		return []string{fmt.Sprintf("can't prune old snapshots of %s: %v", s.Dataset, err)}, nil
	}
	return nil, nil
}

// zfs runs a change as the operator first, which works with zfs allow, then
// through sudo. See design.md, Privilege model.
func (s *Snapper) zfs(ctx context.Context, args ...string) error {
	argv := append([]string{"zfs"}, args...)
	if !s.Root {
		if _, err := s.Run.Run(ctx, host.Cmd{Args: argv, Quiet: true}); err == nil {
			return nil
		}
	}
	_, err := s.Run.Run(ctx, host.Cmd{Args: argv, Sudo: true})
	return err
}

// prune destroys the tool's snapshots that the schedule doesn't keep. taken
// is counted even when a dry run didn't make it, so the plan matches.
func (s *Snapper) prune(ctx context.Context, taken string) error {
	out, err := s.Run.Run(ctx, host.Cmd{Look: true, Args: []string{"zfs", "list", "-H", "-o", "name", "-t", "snapshot", "-d", "1", "--", s.Dataset}})
	if err != nil {
		return err
	}
	names := []string{taken}
	for _, line := range strings.Split(string(out), "\n") {
		ds, snap, ok := strings.Cut(strings.TrimSpace(line), "@")
		if ok && ds == s.Dataset && snap != taken && ours.MatchString(snap) {
			names = append(names, snap)
		}
	}
	gone := Doomed(names)
	if len(gone) == 0 {
		return nil
	}
	// One destroy for the lot: dataset@a,b,c.
	return s.zfs(ctx, "destroy", "--", s.Dataset+"@"+strings.Join(gone, ","))
}

type stamped struct {
	name string
	at   time.Time
}

// Doomed gives the names that the schedule drops, oldest first. Names that
// don't match the tool's pattern are never in it. Only the time in a name
// counts, so a clock change or a server left off for months doesn't empty
// the dataset.
func Doomed(names []string) []string {
	var snaps []stamped
	for _, n := range names {
		m := ours.FindStringSubmatch(n)
		if m == nil {
			continue
		}
		// UTC only so a time skipped by a DST change still parses; just the
		// calendar fields are used.
		at, err := time.Parse(stampLayout, m[1])
		if err != nil {
			continue
		}
		snaps = append(snaps, stamped{n, at})
	}
	sort.Slice(snaps, func(i, j int) bool {
		if !snaps[i].at.Equal(snaps[j].at) {
			return snaps[i].at.Before(snaps[j].at)
		}
		return snaps[i].name < snaps[j].name
	})
	if len(snaps) == 0 {
		return nil
	}
	keep := map[string]bool{snaps[0].name: true}
	byBucket := func(n int, bucket func(time.Time) string) {
		seen := map[string]bool{}
		for i := len(snaps) - 1; i >= 0 && len(seen) < n; i-- {
			b := bucket(snaps[i].at)
			if !seen[b] {
				seen[b] = true
				keep[snaps[i].name] = true
			}
		}
	}
	for _, s := range snaps[max(0, len(snaps)-keepNewest):] {
		keep[s.name] = true
	}
	byBucket(keepDays, func(t time.Time) string { return t.Format("2006-01-02") })
	byBucket(keepWeeks, func(t time.Time) string {
		y, w := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	})
	byBucket(keepMonths, func(t time.Time) string { return t.Format("2006-01") })

	var gone []string
	for _, s := range snaps {
		if !keep[s.name] {
			gone = append(gone, s.name)
		}
	}
	return gone
}
