// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package soak runs an action over and over, and compares the live heap and
// the goroutines after round N with after round 2N. Anything that keeps
// growing past the first N rounds is a leak.
package soak

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Env is the rounds per half, for the soak tests. They skip when it's unset.
const Env = "TT_MINECRAFTD_SOAK"

// Slack is what a soak may grow by between the 2 samples without failing.
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
	r := Result{Rounds: n}
	for i := 0; i < 2*n; i++ {
		if err := round(i); err != nil {
			return r, fmt.Errorf("round %d: %w", i+1, err)
		}
		if i == n-1 {
			r.Half = Take()
		}
	}
	r.Double = Take()
	return r, nil
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

// Rounds is the the rounds per half from Env, or a skip when it isn't set.
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
	start := time.Now()
	r, err := Run(n, round)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s, in %s", r, time.Since(start).Round(time.Millisecond))
	if leak := r.Leak(); leak != "" {
		t.Errorf("%s over rounds %d to %d", leak, n, 2*n)
	}
}
