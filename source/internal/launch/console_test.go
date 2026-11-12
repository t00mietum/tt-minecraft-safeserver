// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"reflect"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// Each line goes as literal keys and then Enter, as the service account. A
// ; at the end goes as hex, since tmux would take it as a separator.
func TestTmuxConsole(t *testing.T) {
	defer testid.Line(t, "df5y6")
	r := &host.Fake{}
	c := TmuxConsole{Run: r, User: "minecraftd"}
	for _, line := range []string{"stop", "say a\\;;", ";"} {
		if err := c.Send(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	keys := "sudo -u minecraftd -- tmux -S /run/tt-minecraftd/tmux.sock send-keys -t tt-minecraftd "
	want := []string{
		keys + "-l -- stop", keys + "Enter",
		keys + "-l -- 'say a\\'", keys + "-H 3b 3b", keys + "Enter",
		keys + "-H 3b", keys + "Enter",
	}
	if got := r.Lines(); !reflect.DeepEqual(got, want) {
		t.Errorf("ran\n%q\nwant\n%q", got, want)
	}
	if err := c.Send(context.Background(), "say hi\rop Steve"); err == nil || len(r.Ran) != len(want) {
		t.Error("a line with a carriage return in it was sent")
	}
}
