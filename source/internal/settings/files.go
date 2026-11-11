// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package settings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// maxFile caps what's read of one settings file.
const maxFile = 8 << 20

// dir is an open folder in the server tree. Everything under it is opened
// relative to it and without following links, since the server account can
// write there, and repair runs as root.
type dir struct {
	fd   int
	path string
}

// openDir opens serverDir itself. That one may be a link, as config allows.
func openDir(path string) (dir, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return dir{}, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return dir{fd, path}, nil
}

func (d dir) close() {
	_ = syscall.Close(d.fd) // read-only folder handle, nothing to lose
}

func (d dir) name(file string) string {
	return d.path + "/" + file
}

func (d dir) sub(name string) (dir, error) {
	fd, err := syscall.Openat(d.fd, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return dir{}, linkErr(d.name(name), err)
	}
	return dir{fd, d.name(name)}, nil
}

// read gives a regular file's text and its owner and mode.
func (d dir) read(name string) ([]byte, *syscall.Stat_t, error) {
	// O_NONBLOCK, so a pipe left in its place doesn't hang the open.
	fd, err := syscall.Openat(d.fd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, linkErr(d.name(name), err)
	}
	f := os.NewFile(uintptr(fd), d.name(name))
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return nil, nil, &fs.PathError{Op: "stat", Path: f.Name(), Err: err}
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, nil, fmt.Errorf("%s isn't a regular file", f.Name())
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, nil, err
	}
	if len(b) > maxFile {
		return nil, nil, fmt.Errorf("%s is over %d MiB, too big for a settings file", f.Name(), maxFile>>20)
	}
	return b, &st, nil
}

// write replaces name with text through a new file and a rename, so a link
// put in its place gets replaced rather than followed. like is the file it
// replaces, for its owner and mode; a new file takes the folder's owner, and
// ug+rw per design.md's Tree ownership.
func (d dir) write(name string, text []byte, like *syscall.Stat_t) error {
	mode, uid, gid := uint32(0o660), -1, -1
	if like != nil {
		mode, uid, gid = like.Mode&0o7777, int(like.Uid), int(like.Gid)
	} else {
		var st syscall.Stat_t
		if err := syscall.Fstat(d.fd, &st); err != nil {
			return &fs.PathError{Op: "stat", Path: d.path, Err: err}
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
		return &fs.PathError{Op: "create", Path: d.name(tmp), Err: err}
	}
	f := os.NewFile(uintptr(fd), d.name(tmp))
	err = writeAll(f, fd, text, mode, uid, gid)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = syscall.Renameat(d.fd, tmp, d.fd, name)
		if err != nil {
			err = &fs.PathError{Op: "rename", Path: d.name(name), Err: err}
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

func linkErr(path string, err error) error {
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return fmt.Errorf("%s is a link or not the right kind of file, which the server tree can't have here", path)
	}
	return &fs.PathError{Op: "open", Path: path, Err: err}
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
