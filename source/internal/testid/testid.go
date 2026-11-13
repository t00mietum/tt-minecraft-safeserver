// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package testid prints the one status line per test that the run log is grepped for.
package testid

import (
	"fmt"
	"testing"
)

// Line prints "PASS <id> <test>", or FAIL or SKIP. Defer it first thing in a
// test or fuzz target, so it runs last and sees a failure, a skip or a panic.
func Line(t testing.TB, id string) {
	r := recover()
	status := "PASS"
	if r != nil || t.Failed() {
		status = "FAIL"
	} else if t.Skipped() {
		status = "SKIP"
	}
	fmt.Printf("%-4s %s %s\n", status, id, t.Name())
	if r != nil {
		panic(r)
	}
}
