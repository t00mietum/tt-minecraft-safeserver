// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package soak runs an action over and over, and compares the live heap and
// the goroutines after round N with after round 2N. Anything that keeps
// growing past the first N rounds is a leak.
package soak

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Env is the rounds per half, for the soak tests. They skip when it's unset.
const Env = "TT_MINECRAFTD_SOAK"

// ProfileEnv names a folder for a heap profile at each sample, so
// go tool pprof -base can show what grew.
const ProfileEnv = "TT_MINECRAFTD_SOAK_PROFILES"

// HeapSlack and GoroutineSlack are what a soak may grow by between the 2
// samples without failing.
// Per round, it's tiny once N is in the hundreds.
var (
	HeapSlack      uint64 = 1 << 20
	GoroutineSlack        = 2
)

// Sample is the live heap and the goroutine count, after a GC.
type Sample struct {
	Heap       uint64
	Goroutines int
}

// Take settles the goroutines first: one that's on its way out, such as a
// closed connection's reader, isn't a leak.
func Take() Sample {
	n, same := runtime.NumGoroutine(), 0
	for i := 0; i < 100 && same < 3; i++ {
		runtime.GC()
		time.Sleep(5 * time.Millisecond)
		if m := runtime.NumGoroutine(); m == n {
			same++
		} else {
			n, same = m, 0
		}
	}
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return Sample{Heap: m.HeapAlloc, Goroutines: runtime.NumGoroutine()}
}

// Result is a soak's 2 samples.
type Result struct {
	Rounds       int
	Half, Double Sample
}

// Run does round n times, takes a sample, does n more, and takes another.
// It stops at the first error.
func Run(n int, round func(i int) error) (Result, error) {
	return run(n, round, nil)
}

// snap, when there is one, runs right after each sample, with "half" or "double".
func run(n int, round func(i int) error, snap func(which string) error) (Result, error) {
	r := Result{Rounds: n}
	for i := 0; i < 2*n; i++ {
		if err := round(i); err != nil {
			return r, fmt.Errorf("round %d: %w", i+1, err)
		}
		if i == n-1 {
			r.Half = Take()
			if snap != nil {
				if err := snap("half"); err != nil {
					return r, err
				}
			}
		}
	}
	r.Double = Take()
	if snap != nil {
		if err := snap("double"); err != nil {
			return r, err
		}
	}
	return r, nil
}

// heapTo writes the heap profile, as of the GC Take just ran, to
// dir/<test binary>_<test>_<which>.pb.gz.
func heapTo(dir, test string) func(string) error {
	bin := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	test = strings.NewReplacer("/", "_", " ", "_").Replace(test)
	return func(which string) error {
		root, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer root.Close()
		f, err := root.Create(bin + "_" + test + "_" + which + ".pb.gz")
		if err != nil {
			return err
		}
		if err := pprof.WriteHeapProfile(f); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
}

// Leak says what grew past the slack, or "" when nothing did.
func (r Result) Leak() string {
	var s string
	if r.Double.Heap > r.Half.Heap+max(HeapSlack, r.Half.Heap/10) {
		s = fmt.Sprintf("live heap grew from %d to %d bytes", r.Half.Heap, r.Double.Heap)
	}
	if r.Double.Goroutines > r.Half.Goroutines+GoroutineSlack {
		if s != "" {
			s += ", and "
		}
		s += fmt.Sprintf("goroutines grew from %d to %d", r.Half.Goroutines, r.Double.Goroutines)
	}
	return s
}

func (r Result) String() string {
	return fmt.Sprintf("rounds %d and %d: heap %d -> %d bytes, goroutines %d -> %d",
		r.Rounds, 2*r.Rounds, r.Half.Heap, r.Double.Heap, r.Half.Goroutines, r.Double.Goroutines)
}

// Rounds is the rounds per half from Env, or a skip when it isn't set.
func Rounds(t testing.TB) int {
	t.Helper()
	v := os.Getenv(Env)
	if v == "" {
		t.Skip(Env + " isn't set")
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("%s=%q isn't a count of rounds", Env, v)
	}
	return n
}

// Check soaks round for Rounds, and fails t on an error or a leak.
func Check(t testing.TB, round func(i int) error) {
	t.Helper()
	n := Rounds(t)
	var snap func(string) error
	if dir := os.Getenv(ProfileEnv); dir != "" {
		snap = heapTo(dir, t.Name())
	}
	start := time.Now()
	r, err := run(n, round, snap)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s, in %s", r, time.Since(start).Round(time.Millisecond))
	if leak := r.Leak(); leak != "" {
		t.Errorf("%s over rounds %d to %d", leak, n, 2*n)
	}
}
