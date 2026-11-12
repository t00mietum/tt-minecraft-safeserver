// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func TestMain(m *testing.M) {
	if os.Getenv("TT_ATTACH_STANDIN") != "" {
		os.Exit(attachStandIn())
	}
	os.Exit(m.Run())
}

// attachStandIn reads its terminal the way attach does: Ctrl-C detaches only
// in a read of its own, and anywhere else it reaches the server.
func attachStandIn() int {
	var t syscall.Termios
	if err := ioctl(os.Stdin, syscall.TCGETS, unsafe.Pointer(&t)); err != nil {
		fmt.Fprintln(os.Stderr, "the input device is not a TTY")
		return 1
	}
	if t.Lflag&(syscall.ICANON|syscall.ISIG|syscall.ECHO) != 0 {
		fmt.Fprintln(os.Stderr, "terminal isn't raw")
		return 4
	}
	fmt.Println("[12:00:00 INFO]: a line of console output")
	mode := os.Getenv("TT_ATTACH_STANDIN")
	if mode == "gone" {
		fmt.Fprintln(os.Stderr, "Error: no container with name or ID \"tt-minecraftd\" found")
		return 125
	}
	var got []byte
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 5
		}
		if n == 1 && buf[0] == 3 {
			break
		}
		if bytes.IndexByte(buf[:n], 3) >= 0 {
			got = append(got, "<interrupt>"...)
			break
		}
		got = append(got, buf[:n]...)
	}
	if err := os.WriteFile(os.Getenv("TT_ATTACH_GOT"), got, 0o600); err != nil {
		return 6
	}
	if mode == "docker" {
		fmt.Fprintln(os.Stderr, "read escape sequence")
		return 1
	}
	return 0
}

// The line reaches a raw terminal with a carriage return, and Ctrl-C comes
// by itself, so it detaches rather than reaching the server.
func TestAttachConsole(t *testing.T) {
	defer testid.Line(t, "df5xw")
	got := filepath.Join(t.TempDir(), "got")
	t.Setenv("TT_ATTACH_GOT", got)
	c := AttachConsole{argv: []string{os.Args[0]}}
	for _, mode := range []string{"podman", "docker"} {
		t.Setenv("TT_ATTACH_STANDIN", mode)
		for _, line := range []string{"stop", "say The server stops in 10 seconds."} {
			if err := os.Remove(got); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := c.Send(context.Background(), line); err != nil {
				t.Fatalf("%s: %v", mode, err)
			}
			if b, err := os.ReadFile(got); err != nil || string(b) != line+"\r" {
				t.Errorf("%s: got %q, %v; want %q and a carriage return", mode, b, err, line)
			}
		}
	}

	t.Setenv("TT_ATTACH_STANDIN", "gone")
	if err := c.Send(context.Background(), "list"); err == nil || !strings.Contains(err.Error(), "no container") {
		t.Errorf("no container: %v", err)
	}
	t.Setenv("TT_ATTACH_STANDIN", "docker")
	if err := c.Send(context.Background(), "say hi\nop Steve"); err == nil {
		t.Error("a line with a newline in it was sent")
	}

	var out bytes.Buffer
	dry := AttachConsole{Runtime: "podman", DryRun: true, Out: &out}
	if err := dry.Send(context.Background(), "stop"); err != nil || out.String() != "would send to the console: stop\n" {
		t.Errorf("dry run: %v, %q", err, out.String())
	}
	want := []string{"sudo", "-n", "podman", "attach", "--sig-proxy=false", "--detach-keys=ctrl-c", "tt-minecraftd"}
	if got := dry.command(); !reflect.DeepEqual(got, want) {
		t.Errorf("command %q", got)
	}
	dry.Root = true
	if got := dry.command(); !reflect.DeepEqual(got, want[2:]) {
		t.Errorf("as root: %q", got)
	}
}
