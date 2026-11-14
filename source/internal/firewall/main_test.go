// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package firewall

import (
	"os"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestMain(m *testing.M) {
	if a := os.Getenv("TT_UDP_STANDIN"); a != "" {
		os.Exit(udpStandIn(strings.Fields(a)))
	}
	testid.Main(m)
}
