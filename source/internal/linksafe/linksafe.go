// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package linksafe opens files in the server tree relative to folder handles,
// without following links. The server account can write there, and repair,
// setup, and update run as root, so a link it plants could point root at any
// file on the host. See design.md, Tree ownership.
package linksafe

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// MaxRead caps what Read takes of one file.
const MaxRead = 8 << 20

// ErrLink is what a link, or the wrong kind of file, in the path gives.
var ErrLink = errors.New("a link or not the right kind of file, which the server tree can't have here")

// Dir is an open folder in the server tree.
type Dir struct {
	fd   int
	path string
}

// Open opens serverDir itself. That one may be a link, as config allows.
func Open(path string) (Dir, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Dir{}, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return Dir{fd, path}, nil
}

// AfterMkdirat is for tests. Mkdir calls it between making a folder and
// opening it, where the server could swap the folder for a link.
var AfterMkdirat func(path string)

// Fd is d's handle, for calls that take one. It stays d's to close.
func (d Dir) Fd() int {
	return d.fd
}

// Close lets go of the handle.
func (d Dir) Close() {
	_ = syscall.Close(d.fd) // read-only folder handle, nothing to lose
}

// Name is file's path under d, for messages.
func (d Dir) Name(file string) string {
	return d.path + "/" + file
}

// Stat gives d's own owner and mode.
func (d Dir) Stat() (*syscall.Stat_t, error) {
	var st syscall.Stat_t
	if err := syscall.Fstat(d.fd, &st); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: d.path, Err: err}
	}
	return &st, nil
}

// Sub opens a folder right under d.
func (d Dir) Sub(name string) (Dir, error) {
	fd, err := syscall.Openat(d.fd, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Dir{}, linkErr(d.Name(name), err)
	}
	return Dir{fd, d.Name(name)}, nil
}

// Walk opens a folder further down, such as plugins/floodgate, one part at a
// time.
func (d Dir) Walk(rel string) (Dir, error) {
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return Dir{}, fmt.Errorf("%s isn't a plain path under %s", rel, d.path)
		}
	}
	cur := d
	for i, p := range parts {
		next, err := cur.Sub(p)
		if i > 0 {
			cur.Close()
		}
		if err != nil {
			return Dir{}, err
		}
		cur = next
	}
	return cur, nil
}

// Mkdir makes a folder under d and opens it. It gets like's owner and group,
// or d's when like is nil, and ug+rwx per design.md's Tree ownership.
func (d Dir) Mkdir(name string, like *syscall.Stat_t) (Dir, error) {
	if like == nil {
		st, err := d.Stat()
		if err != nil {
			return Dir{}, err
		}
		like = st
	}
	if err := syscall.Mkdirat(d.fd, name, 0o700); err != nil {
		return Dir{}, &fs.PathError{Op: "mkdir", Path: d.Name(name), Err: err}
	}
	if AfterMkdirat != nil {
		AfterMkdirat(d.Name(name))
	}
	// The server can swap the new folder for a link before this open, so the
	// owner and mode are set through the handle, never by name.
	sub, err := d.Sub(name)
	if err != nil {
		return Dir{}, err
	}
	if os.Geteuid() == 0 {
		if err := syscall.Fchown(sub.fd, int(like.Uid), int(like.Gid)); err != nil {
			sub.Close()
			return Dir{}, &fs.PathError{Op: "chown", Path: sub.path, Err: err}
		}
	}
	if err := syscall.Fchmod(sub.fd, 0o770); err != nil {
		sub.Close()
		return Dir{}, &fs.PathError{Op: "chmod", Path: sub.path, Err: err}
	}
	return sub, nil
}

// OpenFile opens a file under d to read, with its owner and mode. It may not
// be a regular file; the caller checks.
func (d Dir) OpenFile(name string) (*os.File, *syscall.Stat_t, error) {
	// O_NONBLOCK, so a pipe left in its place doesn't hang the open.
	fd, err := syscall.Openat(d.fd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, linkErr(d.Name(name), err)
	}
	f := os.NewFile(uintptr(fd), d.Name(name))
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		_ = f.Close()
		return nil, nil, &fs.PathError{Op: "stat", Path: f.Name(), Err: err}
	}
	return f, &st, nil
}

// Read gives a regular file's text and its owner and mode.
func (d Dir) Read(name string) ([]byte, *syscall.Stat_t, error) {
	f, st, err := d.OpenFile(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, nil, fmt.Errorf("%s isn't a regular file", f.Name())
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxRead+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > MaxRead {
		return nil, nil, fmt.Errorf("%s is over %d MiB, too big for a settings file", f.Name(), MaxRead>>20)
	}
	return b, st, nil
}

// Write replaces name with text through a new file and a rename, so a link
// put in its place gets replaced rather than followed. like is the file it
// replaces, for its owner and mode; a new file takes the folder's owner, and
// ug+rw per design.md's Tree ownership.
func (d Dir) Write(name string, text []byte, like *syscall.Stat_t) error {
	mode, uid, gid := uint32(0o660), -1, -1
	if like != nil {
		mode, uid, gid = like.Mode&0o7777, int(like.Uid), int(like.Gid)
	} else {
		st, err := d.Stat()
		if err != nil {
			return err
		}
		uid, gid = int(st.Uid), int(st.Gid)
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := "." + name + ".tt-minecraftd-" + hex.EncodeToString(rnd[:])
	fd, err := syscall.Openat(d.fd, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return &fs.PathError{Op: "create", Path: d.Name(tmp), Err: err}
	}
	f := os.NewFile(uintptr(fd), d.Name(tmp))
	err = writeAll(f, fd, text, mode, uid, gid)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = syscall.Renameat(d.fd, tmp, d.fd, name)
		if err != nil {
			err = &fs.PathError{Op: "rename", Path: d.Name(name), Err: err}
		}
	}
	if err != nil {
		_ = syscall.Unlinkat(d.fd, tmp) // best effort; the error that matters is err
	}
	return err
}

func writeAll(f *os.File, fd int, text []byte, mode uint32, uid, gid int) error {
	if _, err := f.Write(text); err != nil {
		return err
	}
	// Only root can give a file away; anyone else made it as the owner already.
	if os.Geteuid() == 0 {
		if err := syscall.Fchown(fd, uid, gid); err != nil {
			return &fs.PathError{Op: "chown", Path: f.Name(), Err: err}
		}
	}
	if err := syscall.Fchmod(fd, mode); err != nil {
		return &fs.PathError{Op: "chmod", Path: f.Name(), Err: err}
	}
	return f.Sync()
}

// renameNoReplace is RENAME_NOREPLACE, which package syscall doesn't have.
const renameNoReplace = 1

// Move renames name under d to newName under to, and never replaces what's
// there: that gives an error that matches fs.ErrExist. A link moves as the
// link.
func (d Dir) Move(name string, to Dir, newName string) error {
	err := renameat2(d.fd, name, to.fd, newName, renameNoReplace)
	if errors.Is(err, syscall.EINVAL) {
		// A filesystem without the flag. The server can still race a name in,
		// but the rename only replaces it, in a folder it can write anyway.
		switch _, err = os.Lstat(to.Name(newName)); {
		case err == nil:
			err = syscall.EEXIST
		case errors.Is(err, fs.ErrNotExist):
			err = syscall.Renameat(d.fd, name, to.fd, newName)
		}
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: d.Name(name), New: to.Name(newName), Err: err}
	}
	return nil
}

// renameat2 is a var so a test can stand in for a filesystem without the flag.
var renameat2 = sysRenameat2Call

func sysRenameat2Call(olddirfd int, oldpath string, newdirfd int, newpath string, flags uint) error {
	from, err := syscall.BytePtrFromString(oldpath)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(newpath)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(sysRenameat2, uintptr(olddirfd), uintptr(unsafe.Pointer(from)), uintptr(newdirfd), uintptr(unsafe.Pointer(to)), uintptr(flags), 0) // #nosec G103 -- renameat2 isn't in package syscall
	if errno != 0 {
		return errno
	}
	return nil
}

func linkErr(path string, err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return fmt.Errorf("%s is %w", path, ErrLink)
	}
	return &fs.PathError{Op: "open", Path: path, Err: err}
}
