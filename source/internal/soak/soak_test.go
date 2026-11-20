// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package soak

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestMain(m *testing.M) { testid.Main(m) }

// A round that keeps a goroutine or some heap fails, and one that keeps
// nothing past round N passes, even when it warms a cache first.
func TestLeak(t *testing.T) {
	defer testid.Line(t, "1a3jp8m")
	const n = 50
	var kept [][]byte
	cache := map[int][]byte{}
	stop := make(chan struct{})
	defer close(stop)
	for _, c := range []struct {
		name  string
		round func(i int) error
		want  string
	}{
		{"clean", func(i int) error { _ = make([]byte, 64<<10); return nil }, ""},
		{"warm cache", func(i int) error {
			if i < n/2 {
				cache[i] = make([]byte, 64<<10)
			}
			return nil
		}, ""},
		{"heap", func(int) error { kept = append(kept, make([]byte, 64<<10)); return nil }, "live heap grew"},
		{"goroutines", func(int) error { go func() { <-stop }(); return nil }, "goroutines grew"},
	} {
		r, err := Run(n, c.round)
		if err != nil {
			t.Fatal(err)
		}
		if leak := r.Leak(); (c.want == "") != (leak == "") || !strings.Contains(leak, c.want) {
			t.Errorf("%s: %q, want %q (%s)", c.name, leak, c.want, r)
		}
	}
	if len(kept) != 2*n || len(cache) != n/2 {
		t.Errorf("kept %d, cached %d", len(kept), len(cache))
	}
}

// With ProfileEnv set, Check leaves a heap profile from each sample, for
// pprof -base.
func TestHeapProfiles(t *testing.T) {
	defer testid.Line(t, "1a4jssw")
	dir := t.TempDir()
	t.Setenv(Env, "3")
	t.Setenv(ProfileEnv, dir)
	Check(t, func(int) error { return nil })
	bin := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	for _, which := range []string{"half", "double"} {
		b, err := os.ReadFile(filepath.Join(dir, bin+"_TestHeapProfiles_"+which+".pb.gz"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(b, []byte{0x1f, 0x8b}) {
			t.Errorf("%s: not a gzipped profile (%d bytes)", which, len(b))
		}
	}
}
