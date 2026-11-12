// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// Console sends one line to the server console, as if typed there.
type Console interface {
	Send(ctx context.Context, line string) error
}

// checkLine refuses a line that would act as more than typed text.
func checkLine(line string) error {
	if strings.IndexFunc(line, unicode.IsControl) >= 0 {
		return fmt.Errorf("console line %q has a control character", line)
	}
	return nil
}

// TmuxConsole types into the service account's tmux session, in native mode.
type TmuxConsole struct {
	Run  host.Runner
	User string
}

func (t TmuxConsole) Send(ctx context.Context, line string) error {
	if err := checkLine(line); err != nil {
		return err
	}
	keys := func(k ...string) error {
		args := append([]string{"tmux", "-S", TmuxSocket, "send-keys", "-t", Container}, k...)
		_, err := t.Run.Run(ctx, host.Cmd{Args: args, As: t.User})
		return err
	}
	// tmux reads a ; that ends an argument as the end of its command, even
	// with -l, so trailing ones go as hex.
	body := strings.TrimRight(line, ";")
	if body != "" {
		if err := keys("-l", "--", body); err != nil {
			return err
		}
	}
	if n := len(line) - len(body); n > 0 {
		hex := []string{"-H"}
		for range n {
			hex = append(hex, "3b")
		}
		if err := keys(hex...); err != nil {
			return err
		}
	}
	return keys("Enter")
}
