// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package download gets jars over https and checks them against the hash
// their download API gives, before they go in the server tree. paper and
// geyser share it.
package download

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"syscall"
	"time"
)

// Client fetches from a download API. The zero value works, with a 10 minute
// timeout and the tool's own user agent.
type Client struct {
	HTTP      *http.Client
	UserAgent string
}

// File is one jar to fetch. Size is 0 when the API doesn't give one.
type File struct {
	Name   string
	URL    string
	SHA256 string
	Size   int64
}

// MaxJSON caps an API answer.
const MaxJSON = 8 << 20

// MaxJar caps a download, whatever size the API gives.
const MaxJar = 512 << 20

// AtSymlinkNoFollow is AT_SYMLINK_NOFOLLOW, which package syscall lacks.
const AtSymlinkNoFollow = 0x100

// OpenDir opens a folder for the *at calls.
func OpenDir(dir string) (int, error) {
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, &fs.PathError{Op: "open", Path: dir, Err: err}
	}
	return fd, nil
}

// SHA256Hex is a hash as the APIs give it, once lower-cased.
var SHA256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (c Client) http() *http.Client {
	if c.HTTP == nil {
		return &http.Client{Timeout: 10 * time.Minute}
	}
	return c.HTTP
}

// Get is a GET that only an OK answers.
func (c Client) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "tt-minecraftd (https://github.com/t00mietum/tt-minecraft-safeserver)"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close() // the status is the error that matters
		return nil, fmt.Errorf("%s: %s", url, Status(resp.StatusCode))
	}
	return resp, nil
}

// Status names an HTTP status by its code. Not resp.Status, whose text is
// the server's own and can have escape codes.
func Status(code int) string {
	if text := http.StatusText(code); text != "" {
		return fmt.Sprintf("%d %s", code, text)
	}
	return fmt.Sprint(code)
}

// JSON reads an API answer of up to MaxJSON into v.
func (c Client) JSON(ctx context.Context, url string, v any) error {
	resp, err := c.Get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxJSON+1))
	if err != nil {
		return err
	}
	if len(b) > MaxJSON {
		return fmt.Errorf("%s: answer over %d MiB", url, MaxJSON>>20)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	return nil
}

// Into downloads f into the folder open as dirfd, named dir in messages. It's
// written to a temp name and renamed once its size and SHA-256 match, so a
// bad download never replaces a good jar. Every op is relative to dirfd,
// without following links, since the server account may own the tree. The
// jar takes the folder's owner and group, mode 0660.
func (c Client) Into(ctx context.Context, dirfd int, dir string, f File) error {
	limit := int64(MaxJar)
	if f.Size > 0 {
		// One over, so a long one says how long.
		limit = f.Size + 1
	}
	s, err := c.Stage(ctx, dirfd, dir, f.Name, f.URL, limit)
	if err != nil {
		return err
	}
	switch {
	case f.Size > 0 && s.Size != f.Size:
		err = fmt.Errorf("%s: got %d bytes, the API said %d", f.Name, s.Size, f.Size)
	case s.SHA256 != f.SHA256:
		err = fmt.Errorf("%s: SHA-256 doesn't match the API's; not used", f.Name)
	}
	if err != nil {
		s.Drop()
		return err
	}
	return s.Keep()
}

// Staged is a download written beside the name it's for, under a temp name,
// until Keep or Drop. Its folder's fd has to stay open until then.
//
// It stays root's, mode 0600, while it waits on checks and the operator's
// yes, so the server account can't rewrite what was hashed. UID, GID and
// Mode are what it gets at Keep, the folder's owner and group and 0660
// unless changed.
type Staged struct {
	dirfd  int
	dir    string
	file   *os.File
	dev    uint64
	ino    uint64
	Name   string
	Temp   string
	Size   int64
	SHA256 string
	SHA512 string
	UID    int
	GID    int
	Mode   uint32
}

// Stage downloads url into the folder open as dirfd, under a temp name beside
// name. More than limit bytes is an error.
func (c Client) Stage(ctx context.Context, dirfd int, dir, name, url string, limit int64) (*Staged, error) {
	var st syscall.Stat_t
	if err := syscall.Fstat(dirfd, &st); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: dir, Err: err}
	}
	resp, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	tmp, err := TempName(name)
	if err != nil {
		return nil, err
	}
	fd, err := syscall.Openat(dirfd, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, &fs.PathError{Op: "create", Path: dir + "/" + tmp, Err: err}
	}
	s := &Staged{dirfd: dirfd, dir: dir, Name: name, Temp: tmp, UID: int(st.Uid), GID: int(st.Gid), Mode: 0o660}
	s.file = os.NewFile(uintptr(fd), dir+"/"+tmp)
	err = s.fill(resp.Body, limit)
	if err == nil {
		err = s.readOnly()
	}
	if err != nil {
		s.Drop()
		return nil, err
	}
	return s, nil
}

// readOnly swaps the write handle for a read one on the same file, since a
// file open for writing can't be run (selfupdate runs its --version).
func (s *Staged) readOnly() error {
	r, err := s.Open()
	if err != nil {
		return err
	}
	if err := s.file.Close(); err != nil {
		_ = r.Close() // the write's error is the one to give
		return err
	}
	s.file = r
	return nil
}

func (s *Staged) fill(body io.Reader, limit int64) error {
	var st syscall.Stat_t
	if err := syscall.Fstat(int(s.file.Fd()), &st); err != nil {
		return err
	}
	s.dev, s.ino = st.Dev, st.Ino
	sum256, sum512 := sha256.New(), sha512.New()
	n, err := io.Copy(io.MultiWriter(s.file, sum256, sum512), io.LimitReader(body, limit+1))
	switch {
	case err != nil:
		return fmt.Errorf("downloading %s: %w", s.Name, err)
	case n > limit && limit < 1<<20:
		return fmt.Errorf("%s: over %d bytes; not used", s.Name, limit)
	case n > limit:
		return fmt.Errorf("%s: over %d MiB; not used", s.Name, limit>>20)
	}
	s.Size = n
	s.SHA256 = hex.EncodeToString(sum256.Sum(nil))
	s.SHA512 = hex.EncodeToString(sum512.Sum(nil))
	return s.file.Sync()
}

// Open gives the staged file, for reading. The server can write the folder,
// so it could put a pipe or its own file in its place: O_NONBLOCK keeps the
// open from waiting on a pipe, and only the file Stage wrote is given back.
func (s *Staged) Open() (*os.File, error) {
	name := s.dir + "/" + s.Temp
	fd, err := syscall.Openat(s.dirfd, s.Temp, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	f := os.NewFile(uintptr(fd), name)
	var st syscall.Stat_t
	err = syscall.Fstat(fd, &st)
	switch {
	case err != nil:
	case st.Mode&syscall.S_IFMT != syscall.S_IFREG:
		err = fmt.Errorf("%s isn't a regular file", name)
	case st.Dev != s.dev || st.Ino != s.ino:
		err = fmt.Errorf("%s was replaced after it was downloaded", name)
	}
	if err != nil {
		_ = f.Close() // only read from
		return nil, err
	}
	return f, nil
}

// Keep gives the download its owner and mode, then renames it over the name
// it's for. A file put in place of the one Stage wrote is refused.
func (s *Staged) Keep() error {
	err := s.ready()
	if err == nil {
		err = syscall.Renameat(s.dirfd, s.Temp, s.dirfd, s.Name)
		if err != nil {
			err = &fs.PathError{Op: "rename", Path: s.dir + "/" + s.Name, Err: err}
		}
	}
	if err != nil {
		s.Drop()
		return err
	}
	_ = s.file.Close() // only read from
	return nil
}

func (s *Staged) ready() error {
	f, err := s.Open()
	if err != nil {
		return err
	}
	_ = f.Close() // only checked
	fd := int(s.file.Fd())
	if os.Geteuid() == 0 {
		if err := syscall.Fchown(fd, s.UID, s.GID); err != nil {
			return fmt.Errorf("chown %s: %w", s.Name, err)
		}
	}
	if err := syscall.Fchmod(fd, s.Mode); err != nil {
		return fmt.Errorf("chmod %s: %w", s.Name, err)
	}
	return nil
}

// Drop removes the download.
func (s *Staged) Drop() {
	_ = syscall.Unlinkat(s.dirfd, s.Temp) // best effort; the caller has the error that matters
	_ = s.file.Close()                    // closed already after a Keep
}

// TempName is a hidden name beside name, for writing before a rename.
func TempName(name string) (string, error) {
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	return "." + name + ".tt-minecraftd-" + hex.EncodeToString(rnd[:]), nil
}
