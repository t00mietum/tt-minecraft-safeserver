// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package firewall

import (
	"context"
	"errors"
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
// "serve <port>..." (answer every datagram to its sender), "probe
// <addr:port>" (send one and say whether anything came back), "send <port>
// <addr:port>" (send one from that port), "listen <port>" (say whether
// anything arrives), "try <addr:port>" (send one and say whether the output
// drop refused it), "rules <path>" (write the rules for this namespace's
// routes), "hear <port>" (say who each datagram came from) or "from <ip>
// <addr:port>" (send one from that address).
func udpStandIn(args []string) int {
	if len(args) < 2 {
		return 2
	}
	switch args[0] {
	case "rules":
		l, err := ReadLocal(Files{ResolvConf: os.DevNull})
		if err != nil {
			fmt.Println(err)
			return 3
		}
		cfg := config.Defaults()
		cfg.RunUser, cfg.Egress = "root", "open"
		if err := os.WriteFile(args[1], []byte(Rules(&cfg, l)), 0o600); err != nil {
			fmt.Println(err)
			return 3
		}
	case "try":
		d := net.Dialer{Control: func(_, _ string, rc syscall.RawConn) error {
			var serr error
			err := rc.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			})
			if err != nil {
				return err
			}
			return serr
		}}
		c, err := d.Dial("udp", args[1])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		_, err = c.Write([]byte("hello"))
		switch {
		case errors.Is(err, syscall.EPERM):
			fmt.Println("refused")
		case err != nil:
			fmt.Println(err)
			return 3
		default:
			fmt.Println("sent")
		}
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
	case "send":
		if len(args) < 3 {
			return 2
		}
		c, err := net.ListenPacket("udp4", "0.0.0.0:"+args[1])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		to, err := net.ResolveUDPAddr("udp4", args[2])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		// An output drop shows up here as EPERM.
		if _, err := c.WriteTo([]byte("hello"), to); errors.Is(err, syscall.EPERM) {
			fmt.Println("refused")
		} else if err != nil {
			fmt.Println(err)
			return 3
		}
	case "listen":
		c, err := net.ListenPacket("udp4", "0.0.0.0:"+args[1])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		fmt.Println("ready")
		_ = c.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
		if _, _, err := c.ReadFrom(make([]byte, 512)); err != nil {
			fmt.Println("none")
		} else {
			fmt.Println("heard")
		}
	case "hear":
		c, err := net.ListenPacket("udp", "[::]:"+args[1])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		fmt.Println("ready")
		_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
		buf := make([]byte, 512)
		for {
			_, from, err := c.ReadFrom(buf)
			if err != nil {
				return 0
			}
			fmt.Println("from", from.(*net.UDPAddr).IP)
		}
	case "from":
		if len(args) < 3 {
			return 2
		}
		c, err := net.ListenPacket("udp", net.JoinHostPort(args[1], "0"))
		if err != nil {
			fmt.Println(err)
			return 3
		}
		to, err := net.ResolveUDPAddr("udp", args[2])
		if err != nil {
			fmt.Println(err)
			return 3
		}
		if _, err := c.WriteTo([]byte("join"), to); err != nil {
			fmt.Println(err)
			return 3
		}
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
// from the server's own address isn't a reply. With the rules loaded, the
// answer to a broadcast on 7551 gets out, and one on 19132 is a new packet
// to the LAN that out_lan drops. A unicast probe still gets its answer, and
// the server can't send from 7551 to a LAN host that didn't ask. The run
// without the rules is the control.
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
	if err := os.WriteFile(rules, []byte(Rules(&cfg, Local{})), 0o600); err != nil {
		t.Fatal(err)
	}
	// 19132 is RakNet's old discovery, which Geyser no longer answers with
	// transport pinned to nethernet.
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
kill $S; wait $S 2>/dev/null || true; S=
TT_UDP_STANDIN="listen 40000" nsenter -t $C -n "$BIN" > "$LLOG" & L=$!
while ! grep -q ready "$LLOG"; do sleep 0.01; done
TT_UDP_STANDIN="send 7551 192.168.77.2:40000" "$BIN"
wait $L
printf 'unasked '; tail -n 1 "$LLOG"
grep got "$LOG" | sort | uniq -c
[ -z "$RULES" ] || /usr/sbin/nft -j list counters inet
`
	for _, load := range []bool{false, true} {
		cmd := exec.Command("unshare", "-rn", "sh", "-c", script)
		cmd.Env = append(os.Environ(), "BIN="+bin, "LOG="+filepath.Join(dir, fmt.Sprint("serve-", load, ".log")), "LLOG="+filepath.Join(dir, fmt.Sprint("listen-", load, ".log")), "RULES=")
		if load {
			cmd.Env[len(cmd.Env)-1] = "RULES=" + rules
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("loaded %v: %v\n%s", load, err, out)
		}
		got := string(out)
		want := []string{"probe 192.168.77.1:19132 reply", "probe 255.255.255.255:19132 reply", "probe 255.255.255.255:7551 reply", "unasked heard"}
		if load {
			want = []string{"probe 192.168.77.1:19132 reply", "probe 255.255.255.255:19132 none", "probe 255.255.255.255:7551 reply", "unasked none"}
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

// Under egress open, the server can't send to a host on the LAN's global
// IPv6 subnet, to multicast, or to broadcast. The rules come from the
// namespace's own routes, where the subnet is on-link, as is a VPN-style
// 2000::/3 and a default route with no gateway, which stay reachable. The
// run without the rules is the control.
func TestLANv6AndMulticastBlocked(t *testing.T) {
	defer testid.Line(t, "df7a4")
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
	tries := []string{"[2001:db8:77::2]:40000", "[ff02::1%vs0]:40000", "224.0.0.251:5353", "255.255.255.255:40000", "192.168.77.2:40000", "[2001:db8:99::1]:40000", "203.0.113.9:40000"}
	script := `set -e
for f in all default; do echo 0 > /proc/sys/net/ipv6/conf/$f/disable_ipv6; done
ip link set lo up
ip link add vs0 type veth peer name vc0
unshare -n sleep 30 & C=$!
trap 'kill $C 2>/dev/null' EXIT
while [ "$(readlink /proc/$C/ns/net)" = "$(readlink /proc/$$/ns/net)" ]; do sleep 0.01; done
ip link set vc0 netns $C
nsenter -t $C -n sh -c 'ip link set lo up && ip link set vc0 up && ip addr add 192.168.77.2/24 dev vc0 && ip -6 addr add 2001:db8:77::2/64 dev vc0 nodad'
ip link set vs0 up
ip addr add 192.168.77.1/24 dev vs0
ip -6 addr add 2001:db8:77::1/64 dev vs0 nodad
ip route add default dev vs0
ip -6 route add default dev vs0
ip -6 route add 2000::/3 dev vs0
if [ -n "$RULES" ]; then
	TT_UDP_STANDIN="rules $RULES" "$BIN"
	/usr/sbin/nft -f "$RULES"
	grep 'elements = { fc00::/7' "$RULES"
fi
for to in ` + strings.Join(tries, " ") + `; do
	printf 'try %s ' "$to"
	TT_UDP_STANDIN="try $to" "$BIN"
done
`
	rules := filepath.Join(t.TempDir(), "firewall.nft")
	for _, load := range []bool{false, true} {
		cmd := exec.Command("unshare", "-rn", "sh", "-c", script)
		cmd.Env = append(os.Environ(), "BIN="+bin, "RULES=")
		if load {
			cmd.Env[len(cmd.Env)-1] = "RULES=" + rules
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("loaded %v: %v\n%s", load, err, out)
		}
		got := string(out)
		for i, to := range tries {
			want := "sent"
			if load && i < 5 {
				want = "refused"
			}
			if w := "try " + to + " " + want + "\n"; !strings.Contains(got, w) {
				t.Errorf("loaded %v: no %q in\n%s", load, w, got)
			}
		}
		if load && !strings.Contains(got, "elements = { fc00::/7, fe80::/10, 2001:db8:77::/64 }") {
			t.Errorf("the subnet isn't in lan6:\n%s", got)
		}
	}
}

// Spoofed sources can fill a rate limit set, and then the update that limits
// an address can't add one. With each set cut to 2, .2 and .3 get their own
// limit of 2, the addresses after them share one, and a tracked address keeps
// its own while the shared one is spent. The same goes for IPv6.
func TestFloodSetFull(t *testing.T) {
	defer testid.Line(t, "df7br")
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
	was := floodSize
	floodSize = 2
	t.Cleanup(func() { floodSize = was })
	cfg := config.Defaults()
	cfg.RunUser = "root"
	cfg.AllowFrom = []string{"lan"}
	cfg.Bedrock = true
	cfg.BedrockPort = 19132
	cfg.JoinRateLimit = 2
	dir := t.TempDir()
	rules := filepath.Join(dir, "firewall.nft")
	if err := os.WriteFile(rules, []byte(Rules(&cfg, Local{})), 0o600); err != nil {
		t.Fatal(err)
	}
	order := "2 3 4 5 6 3 2 2"
	script := `set -e
for f in all default; do echo 0 > /proc/sys/net/ipv6/conf/$f/disable_ipv6; done
ip link set lo up
ip link add vs0 type veth peer name vc0
unshare -n sleep 30 & C=$!
trap 'kill $C ${S:-} 2>/dev/null' EXIT
while [ "$(readlink /proc/$C/ns/net)" = "$(readlink /proc/$$/ns/net)" ]; do sleep 0.01; done
ip link set vc0 netns $C
ip addr add 192.168.77.1/24 dev vs0
ip -6 addr add fd77::1/64 dev vs0 nodad
ip link set vs0 up
nsenter -t $C -n sh -c 'ip link set lo up && ip link set vc0 up && for i in 2 3 4 5 6; do ip addr add 192.168.77.$i/24 dev vc0 && ip -6 addr add fd77::$i/64 dev vc0 nodad; done'
/usr/sbin/nft -f "$RULES"
TT_UDP_STANDIN="hear 19132" "$BIN" > "$LOG" & S=$!
while ! grep -q ready "$LOG"; do sleep 0.01; done
for i in ` + order + `; do TT_UDP_STANDIN="from 192.168.77.$i 192.168.77.1:19132" nsenter -t $C -n "$BIN"; done
for i in ` + order + `; do TT_UDP_STANDIN="from fd77::$i [fd77::1]:19132" nsenter -t $C -n "$BIN"; done
# Loopback skips the limits, and this one comes last.
TT_UDP_STANDIN="from 127.0.0.1 127.0.0.1:19132" "$BIN"
n=0; while ! grep -q 'from 127.0.0.1' "$LOG" && [ $n -lt 500 ]; do sleep 0.01; n=$((n+1)); done
kill $S; wait $S 2>/dev/null || true; S=
grep from "$LOG"
/usr/sbin/nft -j list counters inet
`
	cmd := exec.Command("unshare", "-rn", "sh", "-c", script)
	cmd.Env = append(os.Environ(), "BIN="+bin, "LOG="+filepath.Join(dir, "hear.log"), "RULES="+rules)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := string(out)
	var want []string
	for _, pre := range []string{"192.168.77.", "fd77::"} {
		for _, i := range []string{"2", "3", "4", "5", "3", "2"} {
			want = append(want, "from "+pre+i)
		}
	}
	want = append(want, "from 127.0.0.1")
	if heard := got[:strings.Index(got+"{", "{")]; strings.TrimSpace(heard) != strings.Join(want, "\n") {
		t.Errorf("heard\n%s\nwant\n%s", heard, strings.Join(want, "\n"))
	}
	i := strings.Index(got, "{")
	if i < 0 {
		t.Fatalf("no counters in\n%s", got)
	}
	counts, err := Counters(context.Background(), &host.Fake{Reply: func(host.Cmd) ([]byte, error) { return []byte(got[i:]), nil }})
	if err != nil || counts["in_flood"] != 4 {
		t.Errorf("counters %v, %v, want in_flood 4", counts, err)
	}
}
