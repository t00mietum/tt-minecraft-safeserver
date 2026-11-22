// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package oldbuild

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/linksafe"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestMain(m *testing.M) { testid.Main(m) }

func put(t *testing.T, path, text string, mode os.FileMode, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	return got
}

// A replaced build goes to a new 0755 folder, named for its mtime; a second
// one from the same second gets a suffix, and a link is left where it is.
func TestRetire(t *testing.T) {
	defer testid.Line(t, "1a3jzb8")
	bin := t.TempDir()
	dir := filepath.Join(t.TempDir(), "var-lib", "old-builds")
	mtime := time.Date(2026, 11, 19, 11, 12, 13, 0, time.Local)
	path := filepath.Join(bin, "tt-minecraftd")
	put(t, path, "old build", 0o755, mtime)

	want, err := Where(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := Retire(path, dir, mtime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got != want || filepath.Base(got) != "tt-minecraftd.20261119-111213" {
		t.Errorf("went to %s, Where said %s", got, want)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("the build is still in place: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o755 {
		t.Errorf("folder: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(got); string(b) != "old build" {
		t.Errorf("kept %q", b)
	}

	put(t, path, "another", 0o755, mtime)
	if got, _, err = Retire(path, dir, mtime.Add(time.Hour)); err != nil || filepath.Base(got) != "tt-minecraftd.20261119-111213.2" {
		t.Errorf("same second: %s %v", got, err)
	}

	if err := os.Symlink("/usr/bin/tt-minecraftd", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Retire(path, dir, mtime); err == nil {
		t.Error("moved a link")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("the link is gone: %v", err)
	}

	linked := filepath.Join(t.TempDir(), "old-builds")
	if err := os.Symlink(dir, linked); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(bin, "x"), "x", 0o755, mtime)
	if _, _, err := Retire(filepath.Join(bin, "x"), linked, mtime); err == nil {
		t.Error("took a linked folder")
	}
}

// The build Retire just moved is kept, even when its mtime puts it among
// the ones the rotation prunes, as a copy made with cp -p can. The rest of
// the rotation still runs.
func TestRetireKeepsItsOwn(t *testing.T) {
	defer testid.Line(t, "1a4z4aw")
	bin := t.TempDir()
	dir := filepath.Join(t.TempDir(), "old-builds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, time.Local) }
	put(t, filepath.Join(dir, Name(at(1, 0, 0))), "first", 0o755, at(1, 0, 0))
	// Pruned: not the last of its hour, and older than the newest 10.
	doomed := filepath.Join(dir, Name(at(20, 0, 10)))
	put(t, doomed, "x", 0o755, at(20, 0, 10))
	for h := 1; h <= 11; h++ {
		put(t, filepath.Join(dir, Name(at(20, h, 0))), "x", 0o755, at(20, h, 0))
	}
	path := filepath.Join(bin, "tt-minecraftd")
	put(t, path, "the one being kept", 0o755, at(20, 0, 5))
	got, _, err := Retire(path, dir, at(21, 12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(got); err != nil || string(b) != "the one being kept" {
		t.Errorf("the build Retire just kept: %q %v", b, err)
	}
	if _, err := os.Lstat(doomed); !os.IsNotExist(err) {
		t.Errorf("the rotation didn't run: %v", err)
	}
}

// A rotation that fails once the build is kept is a warning, and the build
// counts as kept. Keep leaves the build in place, with a copy in dir.
func TestRetireRotateFails(t *testing.T) {
	defer testid.Line(t, "1a4z53w")
	bin := t.TempDir()
	dir := filepath.Join(t.TempDir(), "old-builds")
	mtime := time.Date(2026, 11, 21, 10, 30, 0, 0, time.Local)
	path := filepath.Join(bin, "tt-minecraftd")
	FailRotate = errors.New("operation not permitted")
	t.Cleanup(func() { FailRotate = nil })

	put(t, path, "moved", 0o755, mtime)
	got, warn, err := Retire(path, dir, mtime)
	if b, _ := os.ReadFile(got); err != nil || string(b) != "moved" || warn == nil || !strings.Contains(warn.Error(), "couldn't prune the old builds in "+dir+": operation not permitted") {
		t.Errorf("Retire: %s %q, warn %v, err %v", got, b, warn, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("the build is still in place: %v", err)
	}

	put(t, path, "copied", 0o751, mtime)
	got, warn, err = Keep(path, dir, mtime)
	if err != nil || warn == nil || filepath.Base(got) != Name(mtime)+".2" {
		t.Errorf("Keep: %s, warn %v, err %v", got, warn, err)
	}
	fi, err := os.Stat(got)
	if b, _ := os.ReadFile(got); err != nil || string(b) != "copied" || fi.Mode().Perm() != 0o751 || !fi.ModTime().Equal(mtime) {
		t.Errorf("Keep's copy: %q %v %v", b, fi, err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "copied" {
		t.Errorf("Keep took the build away: %q %v", b, err)
	}

	FailRotate = nil
	if _, warn, err := Keep(path, dir, mtime); err != nil || warn != nil {
		t.Errorf("Keep with the rotation working: warn %v, err %v", warn, err)
	}
}

// The copy for a move across filesystems keeps the bytes, the mode and the
// mtime, and leaves no temp file.
func TestCopyOver(t *testing.T) {
	defer testid.Line(t, "1a3jzec")
	src := filepath.Join(t.TempDir(), "tt-minecraftd")
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	put(t, src, "build bytes", 0o751, mtime)
	dir := t.TempDir()
	put(t, filepath.Join(dir, Name(mtime)), "taken", 0o644, mtime)
	to, err := linksafe.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer to.Close()
	name, err := copyOver(src, to, Name(mtime))
	if err != nil || name != Name(mtime)+".2" {
		t.Fatalf("%s %v", name, err)
	}
	fi, err := os.Stat(filepath.Join(dir, name))
	if err != nil || fi.Mode().Perm() != 0o751 || !fi.ModTime().Equal(mtime) {
		t.Errorf("copy: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != "build bytes" {
		t.Errorf("copied %q", b)
	}
	if got := names(t, dir); len(got) != 2 {
		t.Errorf("left %v", got)
	}

	// A real second filesystem, when there's one to hand.
	shm := "/dev/shm"
	if fi, err := os.Stat(shm); err != nil || !fi.IsDir() {
		return
	}
	other, err := os.MkdirTemp(shm, "oldbuild-")
	if err != nil {
		return
	}
	t.Cleanup(func() { _ = os.RemoveAll(other) })
	put(t, filepath.Join(other, "tt-minecraftd"), "far", 0o755, mtime)
	got, _, err := Retire(filepath.Join(other, "tt-minecraftd"), dir, mtime)
	if b, _ := os.ReadFile(got); err != nil || string(b) != "far" {
		t.Errorf("across filesystems: %s %v %q", got, err, b)
	}
	if _, err := os.Lstat(filepath.Join(other, "tt-minecraftd")); !os.IsNotExist(err) {
		t.Errorf("the source is still there: %v", err)
	}
}

// The rotation keeps the newest in each recent finished period, the newest
// 10, and the first, and leaves other names alone. Each kept build here is
// kept by one rule only, so each rule is pinned.
func TestRotate(t *testing.T) {
	defer testid.Line(t, "1a3jzhg")
	dir := t.TempDir()
	at := func(y int, mo time.Month, d, h, m int) time.Time { return time.Date(y, mo, d, h, m, 0, 0, time.UTC) }
	builds := []time.Time{
		at(2020, 3, 1, 0, 0),    // first
		at(2024, 7, 1, 0, 0),    // year
		at(2025, 1, 15, 0, 0),   // month
		at(2025, 2, 1, 0, 0),    // pruned
		at(2025, 2, 10, 0, 0),   // month
		at(2025, 12, 31, 0, 0),  // year, month, week
		at(2026, 10, 5, 0, 0),   // week
		at(2026, 10, 30, 0, 0),  // month, week, day
		at(2026, 11, 10, 8, 0),  // day
		at(2026, 11, 11, 8, 0),  // day
		at(2026, 11, 12, 8, 0),  // day, week
		at(2026, 11, 18, 10, 0), // pruned
		at(2026, 11, 18, 20, 0), // day
	}
	for h := 10; h <= 14; h++ {
		for _, m := range []int{0, 20, 40} {
			builds = append(builds, at(2026, 11, 19, h, m))
		}
	}
	for _, b := range builds {
		put(t, filepath.Join(dir, Name(b)), "x", 0o755, b)
	}
	put(t, filepath.Join(dir, "notes.txt"), "x", 0o644, at(2010, 1, 1, 0, 0))
	// No time in the name, so its mtime counts.
	put(t, filepath.Join(dir, "tt-minecraftd.hand-copy"), "x", 0o755, at(2026, 11, 18, 15, 0))
	if err := os.Symlink("nowhere", filepath.Join(dir, "tt-minecraftd.link")); err != nil {
		t.Fatal(err)
	}

	now := at(2026, 11, 19, 14, 50)
	pruned, err := Rotate(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{Name(at(2025, 2, 1, 0, 0)), Name(at(2026, 11, 18, 10, 0)), "tt-minecraftd.hand-copy"}
	// 10:40 is the last of its hour, and 11:40 is in the newest 10.
	for _, hm := range [][2]int{{10, 0}, {10, 20}, {11, 0}, {11, 20}} {
		want = append(want, Name(at(2026, 11, 19, hm[0], hm[1])))
	}
	slices.Sort(pruned)
	slices.Sort(want)
	if !slices.Equal(pruned, want) {
		t.Errorf("pruned %v\nwant   %v", pruned, want)
	}
	if left := names(t, dir); len(left) != len(builds)+3-len(want) || !slices.Contains(left, "notes.txt") || !slices.Contains(left, "tt-minecraftd.link") {
		t.Errorf("left %v", left)
	}
	if again, err := Rotate(dir, now); err != nil || len(again) != 0 {
		t.Errorf("a second run pruned %v %v", again, err)
	}
}
