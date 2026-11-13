// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package zfs

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A start and a stop every day for almost 2 years, with Nov 12 2026 a
// Thursday in ISO week 46.
func daily(from, to string) []string {
	var names []string
	day, _ := time.Parse("20060102", from)
	end, _ := time.Parse("20060102", to)
	for ; !day.After(end); day = day.AddDate(0, 0, 1) {
		d := day.Format("20060102")
		names = append(names, d+"-080000_"+PreStart, d+"-220000_"+PostStop)
	}
	return names
}

func TestDoomed(t *testing.T) {
	defer testid.Line(t, "df77n")
	names := daily("20250101", "20261112")
	foreign := []string{"manual", "20261112-230000_pre_other", "before-update", "2026111-220000_" + PostStop}
	all := append(slices.Clone(foreign), names...)
	gone := Doomed(all)

	post := func(d string) string { return d + "-220000_" + PostStop }
	want := []string{
		"20250101-080000_" + PreStart,                                     // the first
		"20261112-080000_" + PreStart, post("20261112"), post("20261111"), // newest 3
		post("20261110"), post("20261109"), post("20261108"), post("20261107"), post("20261106"), // days
		post("20261101"), post("20261025"), // weeks 44 and 43
		post("20261031"), post("20260930"), post("20260831"), post("20260731"), post("20260630"), // months
	}
	var kept []string
	for _, n := range names {
		if !slices.Contains(gone, n) {
			kept = append(kept, n)
		}
	}
	slices.Sort(kept)
	slices.Sort(want)
	if !reflect.DeepEqual(kept, want) {
		t.Errorf("kept\n%s\nwant\n%s", strings.Join(kept, "\n"), strings.Join(want, "\n"))
	}
	for _, f := range foreign {
		if slices.Contains(gone, f) {
			t.Errorf("%s isn't the tool's, but would go", f)
		}
	}
	if len(gone)+len(want) != len(names) {
		t.Errorf("%d gone and %d kept of %d", len(gone), len(want), len(names))
	}
	// A second prune finds nothing more.
	if again := Doomed(kept); len(again) != 0 {
		t.Errorf("second prune drops %q", again)
	}

	// A start and a stop in the same second both count among the newest.
	few := []string{"20261112-100000_" + PreStart, "20261112-100000_" + PostStop, "20261112-090000_" + PostStop, "20261112-080000_" + PreStart}
	if got := Doomed(few); len(got) != 0 {
		t.Errorf("4 on one day: drops %q", got)
	}
	if got := Doomed(nil); got != nil {
		t.Errorf("none: %q", got)
	}
}

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
// Root goes straight to it. A failed prune only warns.
func TestTake(t *testing.T) {
	defer testid.Line(t, "df77p")
	now := time.Date(2026, 11, 12, 11, 0, 0, 0, time.Local)
	ctx := context.Background()
	var snaps strings.Builder
	for _, n := range daily("20261020", "20261111") {
		snaps.WriteString("tank/mc@" + n + "\n")
	}
	snaps.WriteString("tank/mc@manual\ntank/mc/child@20261001-080000_" + PreStart + "\n")
	snap := "zfs snapshot -- tank/mc@20261112-110000_" + PreStart
	list := "zfs list -H -o name -t snapshot -d 1 -- tank/mc"

	r := fakeZfs(snaps.String(), nil)
	s := Snapper{Run: r, Dataset: "tank/mc"}
	warns, err := s.Take(ctx, PreStart, now)
	if err != nil || warns != nil {
		t.Fatalf("err %v, warns %q", err, warns)
	}
	lines := r.Lines()
	if len(lines) != 3 || lines[0] != snap || !r.Ran[0].Quiet || lines[1] != list || !r.Ran[1].Look {
		t.Fatalf("ran %q", lines)
	}
	destroy := lines[2]
	for _, d := range []string{"20261021", "20261022", "20261024", "20261029"} {
		if !strings.Contains(destroy, d+"-080000_"+PreStart) || !strings.Contains(destroy, d+"-220000_"+PostStop) {
			t.Errorf("%s not pruned: %s", d, destroy)
		}
	}
	for _, k := range []string{"20261020-080000_" + PreStart + ",", "20261025-220000_", "20261111-220000_", "20261111-080000_", "manual", "child"} {
		if strings.Contains(destroy, k) {
			t.Errorf("%s pruned: %s", k, destroy)
		}
	}
	if !strings.HasPrefix(destroy, "zfs destroy -- tank/mc@20261020-220000_"+PostStop+",") {
		t.Errorf("destroy %s", destroy)
	}

	r = fakeZfs("", func(c host.Cmd) bool { return !c.Sudo && !c.Look })
	s = Snapper{Run: r, Dataset: "tank/mc"}
	if _, err := s.Take(ctx, PostStop, now); err != nil {
		t.Fatal(err)
	}
	want := []string{"zfs snapshot -- tank/mc@20261112-110000_" + PostStop, "sudo zfs snapshot -- tank/mc@20261112-110000_" + PostStop, list}
	if got := r.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("no zfs allow: ran %q, want %q", got, want)
	}

	r = fakeZfs("", nil)
	s = Snapper{Run: r, Dataset: "tank/mc", Root: true}
	if _, err := s.Take(ctx, PreStart, now); err != nil {
		t.Fatal(err)
	}
	if got := r.Lines(); !reflect.DeepEqual(got, []string{"sudo " + snap, list}) {
		t.Errorf("root: ran %q", got)
	}

	r = fakeZfs("", func(c host.Cmd) bool { return c.Args[1] == "snapshot" })
	s = Snapper{Run: r, Dataset: "tank/mc"}
	if _, err := s.Take(ctx, PreStart, now); err == nil || !strings.Contains(err.Error(), "can't snapshot tank/mc") || len(r.Ran) != 2 {
		t.Errorf("snapshot fails: err %v, ran %q", err, r.Lines())
	}

	r = fakeZfs("", func(c host.Cmd) bool { return c.Args[1] == "list" })
	s = Snapper{Run: r, Dataset: "tank/mc"}
	warns, err = s.Take(ctx, PreStart, now)
	if err != nil || len(warns) != 1 || !strings.Contains(warns[0], "can't prune") {
		t.Errorf("list fails: err %v, warns %q", err, warns)
	}

	if _, err := s.Take(ctx, "pre_other", now); err == nil {
		t.Error("an unknown kind was taken")
	}
}

// update --mc-version's snapshot is one of the tool's own, so the schedule
// prunes it like the others.
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
	old := "20261021-120000_" + PreMove
	if gone := Doomed(append(daily("20261020", "20261111"), old)); !slices.Contains(gone, old) {
		t.Errorf("%s kept: %q", old, gone)
	}
}
