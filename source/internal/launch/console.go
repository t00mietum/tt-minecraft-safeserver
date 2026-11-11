// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// Console sends one line to the server console, as if typed there.
type Console interface {
	Send(ctx context.Context, line string) error
}

// DockerConsole writes to the container's input through the Docker API,
// since docker attach refuses input that isn't a terminal.
type DockerConsole struct {
	Socket    string
	Container string
	DryRun    bool
	Out       io.Writer
}

// DockerSocket is the daemon's socket, from a unix:// DOCKER_HOST or the usual place.
func DockerSocket() string {
	if s, ok := strings.CutPrefix(os.Getenv("DOCKER_HOST"), "unix://"); ok && s != "" {
		return s
	}
	return "/var/run/docker.sock"
}

// checkLine refuses a line that would act as more than typed text.
func checkLine(line string) error {
	if strings.IndexFunc(line, unicode.IsControl) >= 0 {
		return fmt.Errorf("console line %q has a control character", line)
	}
	return nil
}

func (d DockerConsole) Send(ctx context.Context, line string) error {
	if err := checkLine(line); err != nil {
		return err
	}
	if d.DryRun {
		fmt.Fprintf(d.Out, "would send to the console: %s\n", line)
		return nil
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", d.Socket)
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	fmt.Fprintf(conn, "POST /containers/%s/attach?stream=1&stdin=1 HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: 0\r\n\r\n", url.PathEscape(d.Container))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return fmt.Errorf("docker attach: %w", err)
	}
	_ = resp.Body.Close() // an upgrade reply has no body to lose
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker attach: %s", resp.Status)
	}
	// A carriage return ends the line both in a raw terminal, as Paper's
	// console sets it, and in a cooked one.
	if _, err := io.WriteString(conn, line+"\r"); err != nil {
		return fmt.Errorf("docker attach: %w", err)
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
