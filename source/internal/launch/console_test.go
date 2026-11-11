// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// A stand-in daemon gets the attach request and then the line.
func TestDockerConsole(t *testing.T) {
	defer testid.Line(t, "df5xw")
	// A socket path has to stay short.
	dir, err := os.MkdirTemp("", "c")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) // made just above
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type seen struct {
		req  *http.Request
		body []byte
	}
	got := make(chan seen, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			got <- seen{}
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		req, err := http.ReadRequest(br)
		if err != nil {
			got <- seen{}
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
		body, _ := io.ReadAll(br)
		got <- seen{req, body}
	}()

	c := DockerConsole{Socket: sock, Container: Container}
	if err := c.Send(context.Background(), "stop"); err != nil {
		t.Fatal(err)
	}
	s := <-got
	if s.req == nil || s.req.Method != "POST" || s.req.URL.String() != "/containers/tt-minecraftd/attach?stream=1&stdin=1" {
		t.Fatalf("request %+v", s.req)
	}
	if string(s.body) != "stop\r" {
		t.Errorf("body %q, want stop and a carriage return", s.body)
	}

	if err := c.Send(context.Background(), "say hi\nop Steve"); err == nil {
		t.Error("a line with a newline in it was sent")
	}
	var out bytes.Buffer
	dry := DockerConsole{Socket: filepath.Join(dir, "none"), DryRun: true, Out: &out}
	if err := dry.Send(context.Background(), "stop"); err != nil || out.String() != "would send to the console: stop\n" {
		t.Errorf("dry run: %v, %q", err, out.String())
	}
}
