// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package main

import (
	"context"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestMain(m *testing.M) {
	// So start and stop never follow the host's own boot unit.
	bootServes = func(context.Context, string) (bool, error) { return false, nil }
	testid.Main(m)
}
