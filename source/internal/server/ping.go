// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// PingReply is the part of a server list ping reply that status shows.
type PingReply struct {
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
		Sample []struct {
			Name string `json:"name"`
		} `json:"sample"`
	} `json:"players"`
}

// pingTimeout is short, since watch runs a ping on every redraw.
const pingTimeout = 3 * time.Second

// maxReply caps what's read back. A real reply with a big icon is far less.
const maxReply = 1 << 20

// Ping asks the server at addr for its version and players, the way the
// game's server list does, so it needs neither RCON nor query.
func Ping(ctx context.Context, addr string) (PingReply, error) {
	var reply PingReply
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return reply, err
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return reply, fmt.Errorf("port %q: %w", portText, err)
	}
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return reply, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return reply, err
		}
	}

	// Handshake with protocol -1, the usual one for a ping, and next state 1
	// for status. Then the status request, which is an empty packet 0.
	var hs bytes.Buffer
	hs.WriteByte(0)
	hs.Write(binary.AppendUvarint(nil, uint64(uint32(0xffffffff))))
	writeString(&hs, host)
	hs.Write(binary.BigEndian.AppendUint16(nil, uint16(port)))
	hs.WriteByte(1)
	var out bytes.Buffer
	writePacket(&out, hs.Bytes())
	writePacket(&out, []byte{0})
	if _, err := conn.Write(out.Bytes()); err != nil {
		return reply, err
	}

	in := bufio.NewReader(conn)
	size, err := binary.ReadUvarint(in)
	if err != nil {
		return reply, fmt.Errorf("reply: %w", err)
	}
	if size == 0 || size > maxReply {
		return reply, fmt.Errorf("reply of %d bytes", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(in, body); err != nil {
		return reply, fmt.Errorf("reply: %w", err)
	}
	r := bytes.NewReader(body)
	id, err := binary.ReadUvarint(r)
	if err != nil || id != 0 {
		return reply, errors.New("reply isn't a status packet")
	}
	n, err := binary.ReadUvarint(r)
	if err != nil || n > uint64(r.Len()) { // #nosec G115 -- a Len is never negative
		return reply, errors.New("reply's text is cut short")
	}
	text := make([]byte, n)
	_, _ = r.Read(text) // n is checked against what's left above
	if err := json.Unmarshal(text, &reply); err != nil {
		return reply, fmt.Errorf("reply: %w", err)
	}
	return reply, nil
}

func writeString(b *bytes.Buffer, s string) {
	b.Write(binary.AppendUvarint(nil, uint64(len(s))))
	b.WriteString(s)
}

func writePacket(b *bytes.Buffer, p []byte) {
	b.Write(binary.AppendUvarint(nil, uint64(len(p))))
	b.Write(p)
}
