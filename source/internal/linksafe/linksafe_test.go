// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package linksafe

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestMain(m *testing.M) { testid.Main(m) }

func open(t *testing.T, path string) Dir {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
}

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// fds counts this process's open handles, to catch one an error path left.
func fds(t *testing.T) int {
	t.Helper()
	e, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(e)
}

func umask(t *testing.T, m int) {
	t.Helper()
	old := syscall.Umask(m)
	t.Cleanup(func() { syscall.Umask(old) })
}

func mode(t *testing.T, path string) (uint32, *syscall.Stat_t) {
	t.Helper()
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		t.Fatal(err)
	}
	return st.Mode & 0o7777, &st
}

// tempLeft names any temp file Write left in dir.
func tempLeft(t *testing.T, dir string) []string {
	t.Helper()
	e, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, f := range e {
		if strings.Contains(f.Name(), ".tt-minecraftd-") {
			left = append(left, f.Name())
		}
	}
	return left
}

// A link, or a file, at any step of the path is refused, and no handle is
// left open on the way out.
func TestWalkRefusesLinks(t *testing.T) {
	defer testid.Line(t, "1a3k048")
	parts := []string{"a", "b", "c"}
	for step := range parts {
		for _, kind := range []string{"link", "file"} {
			top, elsewhere := t.TempDir(), t.TempDir()
			mkdirs(t, filepath.Join(elsewhere, "b", "c"), filepath.Join(top, "a", "b", "c"))
			at := filepath.Join(append([]string{top}, parts[:step+1]...)...)
			if err := os.RemoveAll(at); err != nil {
				t.Fatal(err)
			}
			if kind == "link" {
				// Leads to a real folder with the rest of the path under it.
				link(t, elsewhere, at)
			} else {
				put(t, at, "x")
			}
			d := open(t, top)
			before := fds(t)
			got, err := d.Walk("a/b/c")
			if err == nil {
				got.Close()
				t.Errorf("%s at %s: Walk went through", kind, at)
				continue
			}
			if !errors.Is(err, ErrLink) {
				t.Errorf("%s at %s: %v, want ErrLink", kind, at, err)
			}
			if n := fds(t); n != before {
				t.Errorf("%s at %s: %d handles open after, %d before", kind, at, n, before)
			}
		}
	}

	top := t.TempDir()
	mkdirs(t, filepath.Join(top, "a", "b", "c"))
	d := open(t, top)
	got, err := d.Walk("a/b/c")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name("x") != top+"/a/b/c/x" {
		t.Errorf("Name: %s", got.Name("x"))
	}
	got.Close()
	for _, bad := range []string{"", "a//b", "a/../a", "./a", "a/"} {
		if got, err := d.Walk(bad); err == nil {
			got.Close()
			t.Errorf("Walk(%q) took it", bad)
		}
	}

	// serverDir itself may be a link, as config allows.
	ln := filepath.Join(t.TempDir(), "server")
	link(t, top, ln)
	if d, err := Open(ln); err != nil {
		t.Errorf("Open through a link: %v", err)
	} else {
		d.Close()
	}
}

// Read takes only a regular file that isn't a link, doesn't hang on a pipe,
// and caps the size.
func TestRead(t *testing.T) {
	defer testid.Line(t, "1a3k07c")
	top, elsewhere := t.TempDir(), t.TempDir()
	put(t, filepath.Join(elsewhere, "secret"), "root's")
	link(t, filepath.Join(elsewhere, "secret"), filepath.Join(top, "linked"))
	put(t, filepath.Join(top, "ok"), "text")
	if err := syscall.Mkfifo(filepath.Join(top, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	mkdirs(t, filepath.Join(top, "folder"))
	big := filepath.Join(top, "big")
	put(t, big, "")
	if err := os.Truncate(big, MaxRead+1); err != nil {
		t.Fatal(err)
	}
	d := open(t, top)
	before := fds(t)

	if _, _, err := d.Read("linked"); !errors.Is(err, ErrLink) {
		t.Errorf("link: %v, want ErrLink", err)
	}
	if f, _, err := d.OpenFile("linked"); !errors.Is(err, ErrLink) {
		t.Errorf("OpenFile link: %v, want ErrLink", err)
		if f != nil {
			_ = f.Close()
		}
	}
	for _, name := range []string{"pipe", "folder"} {
		if _, _, err := d.Read(name); err == nil || !strings.Contains(err.Error(), "isn't a regular file") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, _, err := d.Read("big"); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("big: %v", err)
	}
	if _, _, err := d.Read("none"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	b, st, err := d.Read("ok")
	if err != nil || string(b) != "text" || st.Mode&0o777 != 0o600 {
		t.Errorf("ok: %q, %v", b, err)
	}
	if n := fds(t); n != before {
		t.Errorf("%d handles open after, %d before", n, before)
	}
}

// Write replaces a link rather than writing through it, sets the mode it's
// given whatever the umask, and leaves no temp file when the rename fails.
func TestWrite(t *testing.T) {
	defer testid.Line(t, "1a3k0ag")
	umask(t, 0o077)
	top, elsewhere := t.TempDir(), t.TempDir()
	target := filepath.Join(elsewhere, "secret")
	put(t, target, "root's")
	link(t, target, filepath.Join(top, "settings"))
	d := open(t, top)

	if err := d.Write("settings", []byte("new"), nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "root's" {
		t.Errorf("link target now %q", b)
	}
	m, st := mode(t, filepath.Join(top, "settings"))
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || m != 0o660 {
		t.Errorf("new file: mode %o, want a regular file at 0660", st.Mode)
	}

	like := *st
	like.Mode = syscall.S_IFREG | 0o640
	if err := d.Write("settings", []byte("again"), &like); err != nil {
		t.Fatal(err)
	}
	if m, _ := mode(t, filepath.Join(top, "settings")); m != 0o640 {
		t.Errorf("like's mode not kept: %o", m)
	}
	if b, _ := os.ReadFile(filepath.Join(top, "settings")); string(b) != "again" {
		t.Errorf("text: %q", b)
	}

	// A folder with something in it can't be renamed over.
	mkdirs(t, filepath.Join(top, "busy", "x"))
	before := fds(t)
	err := d.Write("busy", []byte("x"), nil)
	if err == nil {
		t.Fatal("Write replaced a folder")
	}
	if left := tempLeft(t, top); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
	if n := fds(t); n != before {
		t.Errorf("%d handles open after, %d before", n, before)
	}
}

// Move never replaces, moves a link as the link, and keeps both promises on
// a filesystem without RENAME_NOREPLACE.
func TestMove(t *testing.T) {
	defer testid.Line(t, "1a3k0dm")
	check := func(t *testing.T) {
		t.Helper()
		top, elsewhere := t.TempDir(), t.TempDir()
		mkdirs(t, filepath.Join(top, "from"), filepath.Join(top, "to"))
		put(t, filepath.Join(top, "from", "a.jar"), "a")
		put(t, filepath.Join(top, "from", "b.jar"), "b")
		put(t, filepath.Join(top, "to", "b.jar"), "old b")
		put(t, filepath.Join(elsewhere, "secret"), "root's")
		link(t, filepath.Join(elsewhere, "secret"), filepath.Join(top, "from", "c.jar"))
		d := open(t, top)
		from, err := d.Sub("from")
		if err != nil {
			t.Fatal(err)
		}
		defer from.Close()
		to, err := d.Sub("to")
		if err != nil {
			t.Fatal(err)
		}
		defer to.Close()

		if err := from.Move("a.jar", to, "a.jar"); err != nil {
			t.Errorf("a.jar: %v", err)
		} else if b, _ := os.ReadFile(filepath.Join(top, "to", "a.jar")); string(b) != "a" {
			t.Errorf("a.jar moved: %q", b)
		}
		err = from.Move("b.jar", to, "b.jar")
		if !errors.Is(err, fs.ErrExist) {
			t.Errorf("b.jar over b.jar: %v, want fs.ErrExist", err)
		}
		if b, _ := os.ReadFile(filepath.Join(top, "to", "b.jar")); string(b) != "old b" {
			t.Errorf("b.jar replaced: %q", b)
		}
		if b, _ := os.ReadFile(filepath.Join(top, "from", "b.jar")); string(b) != "b" {
			t.Errorf("b.jar source: %q", b)
		}
		if err := from.Move("c.jar", to, "c.jar"); err != nil {
			t.Errorf("c.jar: %v", err)
		} else if _, st := mode(t, filepath.Join(top, "to", "c.jar")); st.Mode&syscall.S_IFMT != syscall.S_IFLNK {
			t.Errorf("c.jar didn't move as a link: mode %o", st.Mode)
		}
		if b, _ := os.ReadFile(filepath.Join(elsewhere, "secret")); string(b) != "root's" {
			t.Errorf("link target: %q", b)
		}
	}
	t.Run("noreplace", check)

	calls := 0
	renameat2 = func(int, string, int, string, uint) error {
		calls++
		return syscall.EINVAL
	}
	t.Cleanup(func() { renameat2 = sysRenameat2Call })
	t.Run("einval", check)
	if calls != 3 {
		t.Errorf("stand-in called %d times, want 3", calls)
	}
}

// Mkdir makes the folder ug+rwx whatever the umask, refuses one that's there,
// and refuses one swapped for a link before it's opened.
func TestMkdir(t *testing.T) {
	defer testid.Line(t, "1a3k0gr")
	umask(t, 0o077)
	top, elsewhere := t.TempDir(), t.TempDir()
	d := open(t, top)
	sub, err := d.Mkdir("plugins-refused", nil)
	if err != nil {
		t.Fatal(err)
	}
	sub.Close()
	if m, _ := mode(t, filepath.Join(top, "plugins-refused")); m != 0o770 {
		t.Errorf("mode %o, want 0770", m)
	}
	if sub, err := d.Mkdir("plugins-refused", nil); !errors.Is(err, fs.ErrExist) {
		t.Errorf("again: %v, want fs.ErrExist", err)
		if err == nil {
			sub.Close()
		}
	}

	AfterMkdirat = func(path string) {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		link(t, elsewhere, path)
	}
	t.Cleanup(func() { AfterMkdirat = nil })
	before := fds(t)
	if sub, err := d.Mkdir("swapped", nil); !errors.Is(err, ErrLink) {
		t.Errorf("swapped for a link: %v, want ErrLink", err)
		if err == nil {
			sub.Close()
		}
	}
	if n := fds(t); n != before {
		t.Errorf("%d handles open after, %d before", n, before)
	}
	if m, _ := mode(t, elsewhere); m != 0o700 {
		t.Errorf("link target's mode changed to %o", m)
	}
}

const asRoot = "TT_LINKSAFE_AS_ROOT"

// Mkdir and Write give a new folder or file away only as root. This runs
// itself again as root in a user namespace, with more ids mapped when
// newuidmap allows, so the owner it sets is one the test didn't have.
func TestRootChown(t *testing.T) {
	defer testid.Line(t, "1a3k0kw")
	if os.Getenv(asRoot) != "" {
		rootSide(t)
		return
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("no unshare")
	}
	var args []string
	for _, try := range [][]string{{"-r", "--map-auto"}, {"-r"}} {
		if exec.Command("unshare", append(try, "true")...).Run() == nil { // #nosec G204 -- fixed args
			args = try
			break
		}
	}
	if args == nil {
		t.Skip("no user namespace here")
	}
	args = append(args, os.Args[0], "-test.run=^TestRootChown$", "-test.count=1")
	cmd := exec.Command("unshare", args...) // #nosec G204 -- the test binary itself
	cmd.Env = append(os.Environ(), asRoot+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PASS 1a3k0kw") {
		t.Errorf("as root, unshare %v: %v\n%s", args[:len(args)-3], err, out)
	}
	t.Logf("unshare %v", args[:len(args)-3])
}

func rootSide(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("not root")
	}
	// The child cleans up itself, since what it gives away the test's own
	// account may not be able to remove.
	top, err := os.MkdirTemp("", "linksafe")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(top)
	umask(t, 0o077)

	// uid 1 only when newuidmap mapped more than root.
	id := uint32(0)
	if b, err := os.ReadFile("/proc/self/uid_map"); err == nil && strings.Count(strings.TrimSpace(string(b)), "\n") > 0 {
		id = 1
	}
	d := open(t, top)
	like := &syscall.Stat_t{Uid: id, Gid: id, Mode: syscall.S_IFREG | 0o640}
	sub, err := d.Mkdir("given", like)
	if err != nil {
		t.Fatal(err)
	}
	sub.Close()
	if m, st := mode(t, filepath.Join(top, "given")); st.Uid != id || st.Gid != id || m != 0o770 {
		t.Errorf("Mkdir: %d:%d %o, want %d:%d 0770", st.Uid, st.Gid, m, id, id)
	}
	if err := d.Write("file", []byte("x"), like); err != nil {
		t.Fatal(err)
	}
	if m, st := mode(t, filepath.Join(top, "file")); st.Uid != id || st.Gid != id || m != 0o640 {
		t.Errorf("Write: %d:%d %o, want %d:%d 0640", st.Uid, st.Gid, m, id, id)
	}

	// An id with no mapping makes the chown fail, which tries the error
	// paths: no handle kept, no temp file left.
	none := &syscall.Stat_t{Uid: 3000000000, Gid: 3000000000, Mode: syscall.S_IFREG | 0o640}
	before := fds(t)
	if sub, err := d.Mkdir("refused", none); err == nil || !strings.Contains(err.Error(), "chown") {
		t.Errorf("Mkdir with an unmapped owner: %v", err)
		if err == nil {
			sub.Close()
		}
	}
	if err := d.Write("refused-file", []byte("x"), none); err == nil || !strings.Contains(err.Error(), "chown") {
		t.Errorf("Write with an unmapped owner: %v", err)
	}
	if n := fds(t); n != before {
		t.Errorf("%d handles open after, %d before", n, before)
	}
	if left := tempLeft(t, top); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
	if _, err := os.Lstat(filepath.Join(top, "refused-file")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("refused-file: %v", err)
	}
}
