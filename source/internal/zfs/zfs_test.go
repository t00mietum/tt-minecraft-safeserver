// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package zfs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// The prune schedule went with start and stop snapshots (backlog
// 2026111318002875).
//
// // A start and a stop every day for almost 2 years, with Nov 12 2026 a
// // Thursday in ISO week 46.
// func daily(from, to string) []string {
// 	var names []string
// 	day, _ := time.Parse("20060102", from)
// 	end, _ := time.Parse("20060102", to)
// 	for ; !day.After(end); day = day.AddDate(0, 0, 1) {
// 		d := day.Format("20060102")
// 		names = append(names, d+"-080000_"+PreStart, d+"-220000_"+PostStop)
// 	}
// 	return names
// }
//
// func TestDoomed(t *testing.T) {
// 	defer testid.Line(t, "df77n")
// 	names := daily("20250101", "20261112")
// 	foreign := []string{"manual", "20261112-230000_pre_other", "before-update", "2026111-220000_" + PostStop}
// 	all := append(slices.Clone(foreign), names...)
// 	gone := Doomed(all)
//
// 	post := func(d string) string { return d + "-220000_" + PostStop }
// 	want := []string{
// 		"20250101-080000_" + PreStart,                                     // the first
// 		"20261112-080000_" + PreStart, post("20261112"), post("20261111"), // newest 3
// 		post("20261110"), post("20261109"), post("20261108"), post("20261107"), post("20261106"), // days
// 		post("20261101"), post("20261025"), // weeks 44 and 43
// 		post("20261031"), post("20260930"), post("20260831"), post("20260731"), post("20260630"), // months
// 	}
// 	var kept []string
// 	for _, n := range names {
// 		if !slices.Contains(gone, n) {
// 			kept = append(kept, n)
// 		}
// 	}
// 	slices.Sort(kept)
// 	slices.Sort(want)
// 	if !reflect.DeepEqual(kept, want) {
// 		t.Errorf("kept\n%s\nwant\n%s", strings.Join(kept, "\n"), strings.Join(want, "\n"))
// 	}
// 	for _, f := range foreign {
// 		if slices.Contains(gone, f) {
// 			t.Errorf("%s isn't the tool's, but would go", f)
// 		}
// 	}
// 	if len(gone)+len(want) != len(names) {
// 		t.Errorf("%d gone and %d kept of %d", len(gone), len(want), len(names))
// 	}
// 	// A second prune finds nothing more.
// 	if again := Doomed(kept); len(again) != 0 {
// 		t.Errorf("second prune drops %q", again)
// 	}
//
// 	// A start and a stop in the same second both count among the newest.
// 	few := []string{"20261112-100000_" + PreStart, "20261112-100000_" + PostStop, "20261112-090000_" + PostStop, "20261112-080000_" + PreStart}
// 	if got := Doomed(few); len(got) != 0 {
// 		t.Errorf("4 on one day: drops %q", got)
// 	}
// 	if got := Doomed(nil); got != nil {
// 		t.Errorf("none: %q", got)
// 	}
// }

// fakeZfs answers zfs list with snaps, and fails the commands fail names.
func fakeZfs(snaps string, fail func(c host.Cmd) bool) *host.Fake {
	return &host.Fake{Reply: func(c host.Cmd) ([]byte, error) {
		if fail != nil && fail(c) {
			return nil, errors.New("permission denied")
		}
		if c.Args[1] == "list" {
			return []byte(snaps), nil
		}
		return nil, nil
	}}
}

// As the operator first, which zfs allow lets through, else through sudo.
// Root goes straight to it. Nothing is listed or pruned.
func TestTake(t *testing.T) {
	defer testid.Line(t, "df77p")
	now := time.Date(2026, 11, 12, 11, 0, 0, 0, time.Local)
	ctx := context.Background()
	snap := "zfs snapshot -- tank/mc@20261112-110000_" + PreMove

	r := fakeZfs("", nil)
	s := Snapper{Run: r, Dataset: "tank/mc"}
	warns, err := s.Take(ctx, PreMove, now)
	if err != nil || warns != nil {
		t.Fatalf("err %v, warns %q", err, warns)
	}
	if got := r.Lines(); !reflect.DeepEqual(got, []string{snap}) || !r.Ran[0].Quiet {
		t.Errorf("ran %q", got)
	}

	r = fakeZfs("", func(c host.Cmd) bool { return !c.Sudo })
	s = Snapper{Run: r, Dataset: "tank/mc"}
	if _, err := s.Take(ctx, PreMove, now); err != nil {
		t.Fatal(err)
	}
	if got := r.Lines(); !reflect.DeepEqual(got, []string{snap, "sudo " + snap}) {
		t.Errorf("no zfs allow: ran %q", got)
	}

	r = fakeZfs("", nil)
	s = Snapper{Run: r, Dataset: "tank/mc", Root: true}
	if _, err := s.Take(ctx, PreMove, now); err != nil {
		t.Fatal(err)
	}
	if got := r.Lines(); !reflect.DeepEqual(got, []string{"sudo " + snap}) {
		t.Errorf("root: ran %q", got)
	}

	r = fakeZfs("", func(c host.Cmd) bool { return c.Args[1] == "snapshot" })
	s = Snapper{Run: r, Dataset: "tank/mc"}
	if _, err := s.Take(ctx, PreMove, now); err == nil || !strings.Contains(err.Error(), "can't snapshot tank/mc") || len(r.Ran) != 2 {
		t.Errorf("snapshot fails: err %v, ran %q", err, r.Lines())
	}

	for _, kind := range []string{"pre_other", "pre_tt-minecraftd-start", "post_tt-minecraftd-stop"} {
		if _, err := s.Take(ctx, kind, now); err == nil {
			t.Errorf("kind %s was taken", kind)
		}
	}
}

// update --mc-version's snapshot is named for the move.
func TestPreMove(t *testing.T) {
	defer testid.Line(t, "df78j")
	r := fakeZfs("", nil)
	s := Snapper{Run: r, Dataset: "tank/mc"}
	if _, err := s.Take(context.Background(), PreMove, time.Date(2026, 11, 12, 11, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	if got := r.Lines(); len(got) == 0 || got[0] != "zfs snapshot -- tank/mc@20261112-110000_pre_tt-minecraftd-mc-version" {
		t.Errorf("ran %q", got)
	}
}
