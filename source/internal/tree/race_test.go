// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package tree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A file that's renamed while chown -R and chmod -R walk the tree, as Paper
// does with its region and session files, doesn't fail the hand-over, and
// the mode fix runs even when the ownership fix fails for real (backlog
// 2026112110300195).
func TestHandOverRenames(t *testing.T) {
	defer testid.Line(t, "1a4z6cg")

	// The real commands, on the test's own account, which needs no root.
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	grp, err := user.LookupGroupId(me.Gid)
	if err != nil {
		t.Fatal(err)
	}
	server := deepDir(t, "server")
	for i := range 10 {
		put(t, filepath.Join(server, "world", fmt.Sprintf("r.%d.mca", i)))
	}
	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ctx.Err() == nil; i = (i + 1) % 10 {
			from := filepath.Join(server, "world", fmt.Sprintf("r.%d.mca", i))
			_ = os.Rename(from, from+".tmp") // the loop's own files
			_ = os.Rename(from+".tmp", from)
		}
	}()
	var errOut bytes.Buffer
	r := &host.Exec{Root: true, Out: &errOut, Err: &errOut}
	c := testConfig(server)
	c.RunUser, c.RunGroup = me.Username, grp.Name
	for range 100 {
		if _, err := ToServer(context.Background(), r, c); err != nil {
			t.Errorf("under renames: %v", err)
			break
		}
	}
	stop()
	wg.Wait()
	if errOut.Len() > 0 {
		t.Errorf("printed:\n%s", errOut.String())
	}

	// A real refusal still fails, and chmod still runs after it.
	f := host.Fake{Reply: func(c host.Cmd) ([]byte, error) {
		if c.Args[0] != "chown" {
			return nil, nil
		}
		fmt.Fprintf(c.ErrCopy, "chown: cannot access '%s/a': No such file or directory\nchown: changing ownership of '%[1]s/b': Operation not permitted\n", server)
		return nil, errors.New("exit status 1")
	}}
	_, err = ToServer(context.Background(), &f, testConfig(server))
	if err == nil || !strings.Contains(err.Error(), "Operation not permitted") || len(f.Ran) != 2 || f.Ran[1].Args[0] != "chmod" {
		t.Errorf("refused chown: %v, ran %q", err, f.Lines())
	}

	// Only gone files is no failure.
	f.Reply = func(c host.Cmd) ([]byte, error) {
		fmt.Fprintf(c.ErrCopy, "%s: cannot access '%s/a': No such file or directory\n", c.Args[0], server)
		return nil, errors.New("exit status 1")
	}
	f.Ran = nil
	if _, err := ToServer(context.Background(), &f, testConfig(server)); err != nil || len(f.Ran) != 2 {
		t.Errorf("gone files: %v, ran %q", err, f.Lines())
	}
}

func put(t *testing.T, file string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
