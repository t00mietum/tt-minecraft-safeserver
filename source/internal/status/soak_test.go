// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package status

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/soak"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// pingServer answers every ping on a loopback port with a status reply,
// as a running Paper does, and gives the port.
func pingServer(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	text := `{"version":{"name":"Paper 26.3","protocol":775},"players":{"max":20,"online":2,"sample":[{"name":"Steve","id":"x"}]}}`
	body := append([]byte{0}, binary.AppendUvarint(nil, uint64(len(text)))...)
	body = append(body, text...)
	reply := append(binary.AppendUvarint(nil, uint64(len(body))), body...)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				in := bufio.NewReader(conn)
				// The handshake, then the status request.
				for range 2 {
					n, err := binary.ReadUvarint(in)
					if err != nil {
						return
					}
					if _, err := io.CopyN(io.Discard, in, int64(n)); err != nil { // #nosec G115 -- the client is our own Ping
						return
					}
				}
				_, _ = conn.Write(reply)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// One Probe taking reading after reading of a running server, as watch does
// until Ctrl-C, keeps no heap or goroutines past the first rounds. Every
// part of the reading is read each time: the process table, the cgroup, a
// real ping, the sockets, the drop counters, the player lists, the jars and
// a log that grows. TT_MINECRAFTD_SOAK sets the rounds, and the test skips
// without it.
func TestSoak(t *testing.T) {
	defer testid.Line(t, "1a4zbf0")
	soak.Rounds(t) // skip before the setup
	g := newRig(t)
	port := pingServer(t)
	g.p.Cfg.GamePort = port
	g.p.Ping = server.Ping
	g.p.Root = true
	g.replies["ps -eo"] = boxedPs("/sbin/docker-init")
	g.replies["ss -H"] = strings.ReplaceAll(ssOut, ":25565 ", ":"+strconv.Itoa(port)+" ")
	g.replies["nft -j"] = `{"nftables": [{"metainfo": {"version": "1.1.6", "json_schema_version": 1}}, ` +
		`{"counter": {"family": "inet", "name": "in_flood", "table": "tt_minecraftd", "handle": 2, "packets": 2, "bytes": 120}}]}`
	log, err := os.OpenFile(filepath.Join(g.dir, "logs", "latest.log"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	want := []string{
		"Server:     running in docker mode, as root, for 2h 12m",
		"Players:    2 of 20 online: Steve",
		"Memory:     1.0 GiB of 4.0 GiB cap (25%)",
		"Ports:      tcp *:" + strconv.Itoa(port) + " listening, 2 connections",
		"Firewall:   dropped in: no in_refused counter, 2 over joinRateLimit, no in_closed counter",
		"Whitelist:  3 players",
		"Plugins:    3 jars: plugins/Geyser-Spigot.jar, plugins/floodgate.jar, paper-26.3-244.jar",
		"Boot unit:  enabled",
	}
	var b strings.Builder
	soak.Check(t, func(i int) error {
		g.r.Ran = g.r.Ran[:0]
		if _, err := fmt.Fprintf(log, "[12:01:00 INFO]: round %d\n", i); err != nil {
			return err
		}
		b.Reset()
		if err := g.p.Take(context.Background()).Write(&b, 0); err != nil {
			return err
		}
		out := b.String()
		for _, w := range append(want, fmt.Sprintf("            [12:01:00 INFO]: round %d", i)) {
			if !strings.Contains(out, w+"\n") {
				return fmt.Errorf("reading lacks %q:\n%s", w, out)
			}
		}
		return nil
	})
}
