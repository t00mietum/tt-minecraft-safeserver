// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Command is an agent run as a program, such as a subscription CLI. It gets
// the prompt on stdin and prints its reply.
type Command struct {
	Argv []string
	// Timeout is 15 minutes when zero, since a CLI that reads the web is slow.
	Timeout time.Duration
}

const maxReply = 1 << 20

// geteuid is a var so a test can be root.
var geteuid = os.Geteuid

// Ask runs the program as the account running this, in an empty temp folder
// that's removed after. It never runs as root.
func (c Command) Ask(ctx context.Context, prompt string) (string, error) {
	if geteuid() == 0 {
		return "", errors.New("the update agent doesn't run as root")
	}
	if len(c.Argv) == 0 {
		return "", errors.New("no agentCommand to run")
	}
	dir, err := os.MkdirTemp("", "tt-minecraftd-agent-")
	if err != nil {
		return "", err
	}
	defer func() {
		// A folder left behind in TMPDIR doesn't change the answer.
		_ = os.RemoveAll(dir)
	}()
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...) // #nosec G204 -- agentCommand, from the root-owned config
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	out := &capped{max: maxReply}
	errOut := &capped{max: 2048}
	cmd.Stdout, cmd.Stderr = out, errOut
	// Its own process group, so a timeout also stops what it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%s didn't answer within %s", c.Argv[0], timeout)
	}
	if err != nil {
		if says := clean(errOut.String(), 400); says != "" {
			return "", fmt.Errorf("%s: %w: %s", c.Argv[0], err, says)
		}
		return "", fmt.Errorf("%s: %w", c.Argv[0], err)
	}
	if out.over {
		return "", fmt.Errorf("%s's answer is over %d bytes", c.Argv[0], maxReply)
	}
	return out.String(), nil
}

// capped keeps the first max bytes, and notes that there were more. The
// buffer isn't embedded, since io.Copy would use its ReadFrom and skip the cap.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *capped) String() string { return c.buf.String() }
