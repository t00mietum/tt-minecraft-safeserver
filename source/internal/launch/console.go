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

func (d DockerConsole) Send(ctx context.Context, line string) error {
	if strings.IndexFunc(line, unicode.IsControl) >= 0 {
		return fmt.Errorf("console line %q has a control character", line)
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
