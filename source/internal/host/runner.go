// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

// Package host runs every outside command, so dry run and sudo live in one place.
package host

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Cmd is one outside command.
type Cmd struct {
	Args []string
	// Sudo runs it through sudo, unless the tool already runs as root.
	Sudo bool
	// As runs it as that account instead, through sudo -u, or runuser when
	// the tool is root.
	As string
	// Look marks a command that only reads state, such as docker ps. It still
	// runs under dry run, so the plan printed matches what a real run would do.
	Look bool
	// Tty hands it the terminal, for sudo's password prompt or a console.
	Tty bool
	// Stdin is fed to it, such as a file for install to put in place.
	Stdin []byte
	// Quiet keeps its stderr off the terminal, for a try that has a fallback.
	// The first line goes in the error instead.
	Quiet bool
}

// Runner runs outside commands. Tests use Fake.
type Runner interface {
	Run(ctx context.Context, c Cmd) ([]byte, error)
}

// Exec is the real runner. A Cmd that isn't Tty gets its stdout returned and
// its stderr passed on.
type Exec struct {
	DryRun bool
	Root   bool
	Out    io.Writer
	Err    io.Writer
	// QuietErr keeps a failed command's stderr off the terminal, and puts
	// its first line in the error instead, as watch needs to redraw cleanly.
	QuietErr bool
}

// NewExec gives a runner on the process's own stdout and stderr.
func NewExec(dryRun bool) *Exec {
	return &Exec{DryRun: dryRun, Root: os.Geteuid() == 0, Out: os.Stdout, Err: os.Stderr}
}

func (e *Exec) Run(ctx context.Context, c Cmd) ([]byte, error) {
	if len(c.Args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	argv := c.Args
	switch {
	case c.As != "" && e.Root:
		argv = append([]string{"runuser", "-u", c.As, "--"}, argv...)
	case c.As != "":
		argv = append([]string{"sudo", "-u", c.As, "--"}, argv...)
	case c.Sudo && !e.Root:
		argv = append([]string{"sudo"}, argv...)
	}
	if e.DryRun && !c.Look {
		fmt.Fprintln(e.Out, "would run:", Quote(argv))
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- every caller builds argv from checked config
	cmd.Stderr = e.Err
	if c.Tty {
		cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%s: %w", Quote(argv), err)
		}
		return nil, nil
	}
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var errText strings.Builder
	if e.QuietErr || c.Quiet {
		cmd.Stderr = &errText
	}
	out, err := cmd.Output()
	if err != nil {
		if first, _, _ := strings.Cut(strings.TrimSpace(errText.String()), "\n"); first != "" {
			return out, fmt.Errorf("%s: %w: %s", Quote(argv), err, first)
		}
		return out, fmt.Errorf("%s: %w", Quote(argv), err)
	}
	return out, nil
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// Quote joins argv the way it would be typed into a shell.
func Quote(argv []string) string {
	var b strings.Builder
	for i, a := range argv {
		if i > 0 {
			b.WriteByte(' ')
		}
		if plainArg.MatchString(a) {
			b.WriteString(a)
			continue
		}
		b.WriteString("'" + strings.ReplaceAll(a, "'", `'\''`) + "'")
	}
	return b.String()
}

// Fake records what would run, for tests. Reply, when set, gives each
// command's output.
type Fake struct {
	Ran   []Cmd
	Reply func(c Cmd) ([]byte, error)
}

func (f *Fake) Run(_ context.Context, c Cmd) ([]byte, error) {
	f.Ran = append(f.Ran, c)
	if f.Reply == nil {
		return nil, nil
	}
	return f.Reply(c)
}

// Lines gives each recorded command as it would print, with sudo marked.
func (f *Fake) Lines() []string {
	lines := make([]string, 0, len(f.Ran))
	for _, c := range f.Ran {
		s := Quote(c.Args)
		switch {
		case c.As != "":
			s = "sudo -u " + c.As + " -- " + s
		case c.Sudo:
			s = "sudo " + s
		}
		lines = append(lines, s)
	}
	return lines
}
