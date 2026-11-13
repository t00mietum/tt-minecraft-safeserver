// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/soak"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/status"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// The commands that don't need a server, over and over through run, plus a
// status probe kept between rounds as watch keeps it, keep no heap or
// goroutines past the first rounds. start, stop, console and cmd soak in
// launch. TT_MINECRAFTD_SOAK sets the rounds, and the test skips without it.
func TestSoak(t *testing.T) {
	defer testid.Line(t, "df78f")
	soak.Rounds(t) // skip before the setup
	noDefaultConfig(t)
	noHostChecks(t)
	fakePaperMC(t)
	fakeReleases(t)
	old := asRoot
	asRoot = func([]string) error { return nil }
	t.Cleanup(func() { asRoot = old })
	// Nothing on the host can run, and setup and update have to take the
	// stand-in runner.
	fake, took := noHostRunner(t)

	dir := t.TempDir()
	if err := os.Symlink("paper-26.3-244.jar", filepath.Join(dir, "paper.jar")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "paper-26.3-244.jar"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "config.shcl")
	if err := os.WriteFile(conf, []byte("serverDir: "+dir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// paper-26.3-244.jar, approved.
	list := "jar: paper-26.3-244.jar\n\tsha256: cba06b5736faf67e54b07b561eae94395e774c517a7d910a54369e1263ccfbd4\n"
	if err := os.WriteFile(filepath.Join(dir, "plugins.shcl"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, _, done := prepare([]string{"repair", "--config", conf}, &bytes.Buffer{}, &bytes.Buffer{})
	if done {
		t.Fatal("prepare stopped")
	}
	var fixed bytes.Buffer
	if err := repair(context.Background(), inv, strings.NewReader(""), &fixed, true); err != nil {
		t.Fatal(err, fixed.String())
	}
	cfg := config.Defaults()
	cfg.ServerDir, cfg.OwnerTrees = dir, nil
	probe := status.NewProbe(&host.Fake{}, &cfg, filepath.Join(dir, "plugins.shcl"))

	steps := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--version"}, exitOK, version},
		{[]string{"--help"}, exitOK, "cmd"},
		{[]string{"repair", "--check", "--config", conf}, exitOK, "OK      safe settings"},
		{[]string{"update", "--dry-run", "--config", conf}, exitOK, "from build 244 to build 249"},
		{[]string{"setup", "--dry-run", "--config", conf}, exitOK, "Found an install at"},
		{[]string{"start", "--bogus"}, exitUsage, ""},
	}
	var b bytes.Buffer
	soak.Check(t, func(i int) error {
		fake.Ran, *took = fake.Ran[:0], 0
		for _, s := range steps {
			code, out, msg := runArgs(s.args...)
			if code != s.code || !strings.Contains(out+msg, s.want) {
				return fmt.Errorf("%q: exit %d, stdout %q, stderr %q", s.args, code, out, msg)
			}
		}
		// update, and setup with its own update.
		if *took != 3 {
			return fmt.Errorf("the stand-in runner was taken %d times, not 3", *took)
		}
		b.Reset()
		if err := probe.Take(context.Background()).Write(&b, 80); err != nil || !strings.Contains(b.String(), "not running") {
			return fmt.Errorf("status: %v, %q", err, b.String())
		}
		return nil
	})
}
