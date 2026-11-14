// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package firewall

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/config"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

// udpStandIn is the test binary run again inside a network namespace, as
// "serve <port>..." (answer every datagram to its sender) or "probe
// <addr:port>" (send one and say whether anything came back).
func udpStandIn(args []string) int {
	if len(args) < 2 {
		return 2
	}
	switch args[0] {
	case "serve":
		for _, p := range args[1:] {
			c, err := net.ListenPacket("udp4", "0.0.0.0:"+p)
			if err != nil {
				fmt.Println(err)
				return 3
			}
			go func() {
				buf := make([]byte, 512)
				for {
					_, from, err := c.ReadFrom(buf)
					if err != nil {
						return
					}
					fmt.Println("got", p)
					_, _ = c.WriteTo([]byte("pong"), from)
				}
			}()
		}
		fmt.Println("ready")
		time.Sleep(time.Minute)
	case "probe":
		lc := net.ListenConfig{Control: func(_, _ string, rc syscall.RawConn) error {
			var serr error
			err := rc.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			})
			if err != nil {
				return err
			}
			return serr
		}}
		c, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:0")
		if err != nil {
			fmt.Println(err)
			return 3
		}
		to, err := net.ResolveUDPAddr("udp4", args[1])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		if _, err := c.WriteTo([]byte("ping"), to); err != nil {
			fmt.Println(err)
			return 3
		}
		_ = c.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
		if _, _, err := c.ReadFrom(make([]byte, 512)); err != nil {
			fmt.Println("none")
		} else {
			fmt.Println("reply")
		}
	}
	return 0
}

// LAN discovery: a client broadcasts, and the server answers it straight
// back. Conntrack keys the probe on the broadcast address, so the answer
// from the server's own address isn't a reply. With the rules loaded it's a
// new packet to the LAN, and out_lan drops it. A unicast probe of the same
// port still gets its answer. The run without the rules is the control.
func TestBroadcastAnswer(t *testing.T) {
	defer testid.Line(t, "df797")
	if !nftCheck(t, "") {
		t.Skip("no unprivileged network namespace with nft")
	}
	for _, tool := range []string{"ip", "nsenter"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	// The test runs as uid 0 in its own user namespace.
	cfg.RunUser = "root"
	cfg.AllowFrom = []string{"lan"}
	cfg.Bedrock = true
	cfg.BedrockPort = 19132
	dir := t.TempDir()
	rules := filepath.Join(dir, "firewall.nft")
	if err := os.WriteFile(rules, []byte(Rules(&cfg, nil)), 0o600); err != nil {
		t.Fatal(err)
	}
	// 7551 is Bedrock's newer LAN discovery port, which Geyser doesn't
	// answer yet. It isn't a game port, so only the answer is checked.
	probes := []string{"192.168.77.1:19132", "255.255.255.255:19132", "255.255.255.255:7551"}
	script := `set -e
ip link set lo up
ip link add vs0 type veth peer name vc0
unshare -n sleep 30 & C=$!
# A failed step would leave these holding the output pipe.
trap 'kill $C ${S:-} 2>/dev/null' EXIT
while [ "$(readlink /proc/$C/ns/net)" = "$(readlink /proc/$$/ns/net)" ]; do sleep 0.01; done
ip link set vc0 netns $C
ip addr add 192.168.77.1/24 dev vs0
ip link set vs0 up
nsenter -t $C -n sh -c 'ip link set lo up && ip addr add 192.168.77.2/24 dev vc0 && ip link set vc0 up && ip route add default dev vc0'
[ -z "$RULES" ] || /usr/sbin/nft -f "$RULES"
TT_UDP_STANDIN="serve 19132 7551" "$BIN" > "$LOG" & S=$!
while ! grep -q ready "$LOG"; do sleep 0.01; done
for to in ` + strings.Join(probes, " ") + `; do
	printf 'probe %s ' "$to"
	TT_UDP_STANDIN="probe $to" nsenter -t $C -n "$BIN"
done
grep got "$LOG" | sort | uniq -c
[ -z "$RULES" ] || /usr/sbin/nft -j list counters inet
`
	for _, load := range []bool{false, true} {
		cmd := exec.Command("unshare", "-rn", "sh", "-c", script)
		cmd.Env = append(os.Environ(), "BIN="+bin, "LOG="+filepath.Join(dir, fmt.Sprint("serve-", load, ".log")), "RULES=")
		if load {
			cmd.Env[len(cmd.Env)-1] = "RULES=" + rules
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("loaded %v: %v\n%s", load, err, out)
		}
		got := string(out)
		want := []string{"probe 192.168.77.1:19132 reply", "probe 255.255.255.255:19132 reply", "probe 255.255.255.255:7551 reply"}
		if load {
			want = []string{"probe 192.168.77.1:19132 reply", "probe 255.255.255.255:19132 none", "probe 255.255.255.255:7551 none"}
		}
		// Every probe reached the server, so what's missing is its answer.
		want = append(want, "2 got 19132", "1 got 7551")
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("loaded %v: no %q in\n%s", load, w, got)
			}
		}
		if !load {
			continue
		}
		list := got[strings.Index(got, "{"):]
		counts, err := Counters(context.Background(), &host.Fake{Reply: func(host.Cmd) ([]byte, error) { return []byte(list), nil }})
		wantCounts := map[string]uint64{"in_refused": 0, "in_flood": 0, "in_closed": 0, "out_local": 0, "out_lan": 2, "out_web": 0}
		if err != nil || !reflect.DeepEqual(counts, wantCounts) {
			t.Errorf("counters %v, %v, want %v", counts, err, wantCounts)
		}
	}
}
