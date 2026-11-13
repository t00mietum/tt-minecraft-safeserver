// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package testid

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

// Main is a package's TestMain. It runs the tests, then fails the package if
// a goroutine is still running. CICD_NO_LEAKCHECK turns the check off.
func Main(m *testing.M) {
	if os.Getenv("CICD_NO_LEAKCHECK") != "" {
		os.Exit(m.Run())
	}
	goleak.VerifyTestMain(m)
}
