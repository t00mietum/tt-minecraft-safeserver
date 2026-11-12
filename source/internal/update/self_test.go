// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package update

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/selfupdate"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

type fakeSelf struct {
	newest    string
	err       error
	installed []string
}

func (f *fakeSelf) Newest(_ context.Context, current string) (selfupdate.Release, bool, error) {
	if f.err != nil || f.newest == current {
		return selfupdate.Release{}, false, f.err
	}
	return selfupdate.Release{Tag: f.newest, Asset: "tt-minecraftd-linux-amd64"}, true, nil
}

func (f *fakeSelf) Install(_ context.Context, exe string, r selfupdate.Release) error {
	f.installed = append(f.installed, exe+" "+r.Tag)
	return nil
}

func selfRig(t *testing.T, answers, newest string) (*rig, *fakeSelf) {
	t.Helper()
	r := newRig(t, answers, 244)
	f := &fakeSelf{newest: newest}
	r.u.Self, r.u.Exe, r.u.Version = f, "/usr/local/bin/tt-minecraftd", "v1.0.0-alpha.1"
	r.u.Parts = Parts{Self: true}
	return r, f
}

// --self asks, then swaps the binary in. It needs no server folder, says
// what to change on NixOS, and is part of a plain update.
func TestSelf(t *testing.T) {
	defer testid.Line(t, "df77m")
	ctx := context.Background()

	r, f := selfRig(t, "y\n", "v1.0.0-alpha.2")
	r.u.Cfg.ServerDir = filepath.Join(t.TempDir(), "none")
	if err := r.u.Do(ctx); err != nil || !reflect.DeepEqual(f.installed, []string{"/usr/local/bin/tt-minecraftd v1.0.0-alpha.2"}) {
		t.Fatalf("%v, installed %q\n%s", err, f.installed, r.out)
	}
	for _, want := range []string{"Move tt-minecraftd from v1.0.0-alpha.1 to v1.0.0-alpha.2", "rename it over /usr/local/bin/tt-minecraftd", "Go ahead?", "FIXED   tt-minecraftd v1.0.0-alpha.2 downloaded"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("no %q:\n%s", want, r.out)
		}
	}
	if strings.Contains(r.out.String(), "next start") {
		t.Errorf("the server note came with only --self:\n%s", r.out)
	}

	r, f = selfRig(t, "n\n", "v1.0.0-alpha.2")
	if err := r.u.Do(ctx); err == nil || len(f.installed) > 0 {
		t.Errorf("a no went on: %v, installed %q", err, f.installed)
	}

	r, f = selfRig(t, "", "v1.0.0-alpha.2")
	r.u.DryRun = true
	if err := r.u.Do(ctx); err != nil || len(f.installed) > 0 || !strings.Contains(r.out.String(), "would download tt-minecraftd v1.0.0-alpha.2 and rename it over /usr/local/bin/tt-minecraftd") {
		t.Errorf("dry run: %v, installed %q\n%s", err, f.installed, r.out)
	}

	r, f = selfRig(t, "", "v1.0.0-alpha.1")
	if err := r.u.Do(ctx); err != nil || len(f.installed) > 0 || !strings.Contains(r.out.String(), "OK      tt-minecraftd v1.0.0-alpha.1 is the newest release") || strings.Contains(r.out.String(), "Go ahead") {
		t.Errorf("up to date: %v, installed %q\n%s", err, f.installed, r.out)
	}

	r, f = selfRig(t, "", "v1.0.0-alpha.2")
	r.u.NixOS = true
	if err := r.u.Do(ctx); err != nil || len(f.installed) > 0 || !strings.Contains(r.out.String(), "SKIPPED tt-minecraftd v1.0.0-alpha.2 is out") || !strings.Contains(r.out.String(), "nixos-rebuild switch") {
		t.Errorf("NixOS: %v, installed %q\n%s", err, f.installed, r.out)
	}

	r, f = selfRig(t, "", "v1.0.0-alpha.2")
	f.err = errors.New("rate limited")
	if err := r.u.Do(ctx); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("a failed lookup: %v", err)
	}

	r, f = selfRig(t, "", "v1.0.0-alpha.2")
	r.u.Exe = ""
	if err := r.u.Do(ctx); err == nil || len(f.installed) > 0 {
		t.Errorf("no binary to replace: %v", err)
	}

	// All parts: Paper's build 249 and this release, in one go-ahead.
	r, f = selfRig(t, "y\n", "v1.0.0-alpha.2")
	r.p.newest = 249
	r.u.Parts = Parts{}
	r.u.Cfg.RunMode = "native"
	if err := r.u.Do(ctx); err != nil || len(f.installed) != 1 || len(r.p.installed) != 1 || strings.Count(r.out.String(), "Go ahead?") != 1 {
		t.Errorf("all parts: %v, installed %q and %q\n%s", err, f.installed, r.p.installed, r.out)
	}
}
