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
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
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
type Staged struct {
	dirfd  int
	dir    string
	Name   string
	Temp   string
	Size   int64
	SHA256 string
	SHA512 string
}

// Stage downloads url into the folder open as dirfd, under a temp name beside
// name, with the folder's owner and group and mode 0660. More than limit bytes
// is an error.
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
	s := &Staged{dirfd: dirfd, dir: dir, Name: name, Temp: tmp}
	file := os.NewFile(uintptr(fd), dir+"/"+tmp)
	err = s.fill(file, fd, resp.Body, limit, st)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.Drop()
		return nil, err
	}
	return s, nil
}

func (s *Staged) fill(file *os.File, fd int, body io.Reader, limit int64, st syscall.Stat_t) error {
	sum256, sum512 := sha256.New(), sha512.New()
	n, err := io.Copy(io.MultiWriter(file, sum256, sum512), io.LimitReader(body, limit+1))
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
	if os.Geteuid() == 0 {
		if err := syscall.Fchown(fd, int(st.Uid), int(st.Gid)); err != nil {
			return fmt.Errorf("chown %s: %w", s.Name, err)
		}
	}
	if err := syscall.Fchmod(fd, 0o660); err != nil {
		return fmt.Errorf("chmod %s: %w", s.Name, err)
	}
	return file.Sync()
}

// Open gives the staged file, for reading.
func (s *Staged) Open() (*os.File, error) {
	fd, err := syscall.Openat(s.dirfd, s.Temp, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: s.dir + "/" + s.Temp, Err: err}
	}
	return os.NewFile(uintptr(fd), s.dir+"/"+s.Temp), nil
}

// Keep renames the download over the name it's for.
func (s *Staged) Keep() error {
	if err := syscall.Renameat(s.dirfd, s.Temp, s.dirfd, s.Name); err != nil {
		s.Drop()
		return &fs.PathError{Op: "rename", Path: s.dir + "/" + s.Name, Err: err}
	}
	return nil
}

// Drop removes the download.
func (s *Staged) Drop() {
	_ = syscall.Unlinkat(s.dirfd, s.Temp) // best effort; the caller has the error that matters
}

// TempName is a hidden name beside name, for writing before a rename.
func TempName(name string) (string, error) {
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	return "." + name + ".tt-minecraftd-" + hex.EncodeToString(rnd[:]), nil
}
