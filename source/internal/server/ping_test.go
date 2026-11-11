// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// fakeServer answers one ping with reply, after checking the handshake.
func fakeServer(t *testing.T, reply []byte) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		in := bufio.NewReader(conn)
		for _, want := range []string{"handshake", "request"} {
			n, err := binary.ReadUvarint(in)
			if err != nil {
				t.Errorf("%s: %v", want, err)
				return
			}
			p := make([]byte, n)
			if _, err := io.ReadFull(in, p); err != nil {
				t.Errorf("%s: %v", want, err)
				return
			}
			// id 0, protocol -1 as 5 bytes, then the host; next state 1 last.
			if want == "handshake" && (p[0] != 0 || !bytes.HasPrefix(p[1:], []byte{0xff, 0xff, 0xff, 0xff, 0x0f}) || p[len(p)-1] != 1) {
				t.Errorf("handshake % x", p)
			}
		}
		_, _ = conn.Write(reply)
	}()
	return ln.Addr().String()
}

func packet(body []byte) []byte {
	return append(binary.AppendUvarint(nil, uint64(len(body))), body...)
}

func TestPing(t *testing.T) {
	defer testid.Line(t, "df5yc")
	text := `{"version":{"name":"Paper 26.3","protocol":775},"players":{"max":20,"online":1,"sample":[{"name":"Steve","id":"x"}]},"description":{"text":"hi"},"favicon":"data:` + strings.Repeat("A", 40000) + `"}`
	body := append([]byte{0}, binary.AppendUvarint(nil, uint64(len(text)))...)
	body = append(body, text...)
	r, err := Ping(context.Background(), fakeServer(t, packet(body)))
	if err != nil || r.Version.Name != "Paper 26.3" || r.Players.Online != 1 || r.Players.Max != 20 || len(r.Players.Sample) != 1 || r.Players.Sample[0].Name != "Steve" {
		t.Errorf("Ping = %+v, %v", r, err)
	}

	for name, reply := range map[string][]byte{
		"wrong id":   packet([]byte{1, 2, '{', '}'}),
		"short text": packet([]byte{0, 50, '{', '}'}),
		"not json":   packet([]byte{0, 3, 'n', 'o', 'p'}),
		"huge":       binary.AppendUvarint(nil, 1<<30),
		"cut off":    binary.AppendUvarint(nil, 100),
		"nothing":    nil,
	} {
		if _, err := Ping(context.Background(), fakeServer(t, reply)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A server that never answers costs the timeout, not a hang.
func TestPingTimeout(t *testing.T) {
	defer testid.Line(t, "df5yd")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Ping(ctx, ln.Addr().String()); err == nil {
		t.Error("no error from a silent server")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %s", d)
	}
}
