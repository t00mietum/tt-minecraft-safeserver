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
	var st syscall.Stat_t
	if err := syscall.Fstat(dirfd, &st); err != nil {
		return &fs.PathError{Op: "stat", Path: dir, Err: err}
	}
	resp, err := c.Get(ctx, f.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp, err := TempName(f.Name)
	if err != nil {
		return err
	}
	fd, err := syscall.Openat(dirfd, tmp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return &fs.PathError{Op: "create", Path: dir + "/" + tmp, Err: err}
	}
	file := os.NewFile(uintptr(fd), dir+"/"+tmp)
	err = fill(file, fd, resp.Body, f, st)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = syscall.Renameat(dirfd, tmp, dirfd, f.Name)
	}
	if err != nil {
		_ = syscall.Unlinkat(dirfd, tmp) // best effort; err is what matters
		return err
	}
	return nil
}

func fill(file *os.File, fd int, body io.Reader, f File, st syscall.Stat_t) error {
	limit := f.Size
	if limit <= 0 {
		limit = MaxJar
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, sum), io.LimitReader(body, limit+1))
	switch {
	case err != nil:
		return fmt.Errorf("downloading %s: %w", f.Name, err)
	case f.Size > 0 && n != f.Size:
		return fmt.Errorf("%s: got %d bytes, the API said %d", f.Name, n, f.Size)
	case n > limit:
		return fmt.Errorf("%s: over %d MiB; not used", f.Name, MaxJar>>20)
	case hex.EncodeToString(sum.Sum(nil)) != f.SHA256:
		return fmt.Errorf("%s: SHA-256 doesn't match the API's; not used", f.Name)
	}
	if os.Geteuid() == 0 {
		if err := syscall.Fchown(fd, int(st.Uid), int(st.Gid)); err != nil {
			return fmt.Errorf("chown %s: %w", f.Name, err)
		}
	}
	if err := syscall.Fchmod(fd, 0o660); err != nil {
		return fmt.Errorf("chmod %s: %w", f.Name, err)
	}
	return file.Sync()
}

// TempName is a hidden name beside name, for writing before a rename.
func TempName(name string) (string, error) {
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	return "." + name + ".tt-minecraftd-" + hex.EncodeToString(rnd[:]), nil
}
