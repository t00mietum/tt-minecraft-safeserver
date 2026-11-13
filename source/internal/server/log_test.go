// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A name or chat line can't hold escape codes or flip the text direction of
// what the operator reads.
func TestClean(t *testing.T) {
	defer testid.Line(t, "df78h")
	for _, c := range []struct{ in, want string }{
		{"Alex", "Alex"},
		{"Mal\x1b[2Jory\x7f\u0085", "Mal[2Jory"},
		{"evil\u202egpj.exe", "evilgpj.exe"},
		{"\u202a\u202b\u202c\u202d\u2066\u2067\u2068\u2069x", "x"},
		// Right-to-left text itself stays, as do characters next to the range.
		{"\u05e9\u05dc\u05d5\u05dd \u2029\u2065\u206a", "\u05e9\u05dc\u05d5\u05dd \u2029\u2065\u206a"},
	} {
		if got := Clean(c.in); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
