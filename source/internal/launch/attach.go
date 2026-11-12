// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
)

// errExited is attach ending before it read the line.
var errExited = errors.New("attach exited")

// attachTimeout bounds one line, from attach to detach.
const attachTimeout = 15 * time.Second

// lineArrive is how long a line written to the pty may take to show up on
// the other end.
const lineArrive = 100 * time.Millisecond

// AttachConsole types a line into the container's terminal through the
// runtime's attach, on a pseudo-terminal of its own. attach refuses a pipe
// under Docker, and never detaches from one under Podman. See design.md,
// console and cmd.
type AttachConsole struct {
	Runtime string
	Root    bool
	DryRun  bool
	Out     io.Writer
	// argv replaces the attach command, for tests.
	argv []string
}

func (a AttachConsole) Send(ctx context.Context, line string) error {
	if err := checkLine(line); err != nil {
		return err
	}
	if a.DryRun {
		fmt.Fprintf(a.Out, "would send to the console: %s\n", line)
		return nil
	}
	argv := a.command()
	master, slave, err := openPty()
	if err != nil {
		return fmt.Errorf("console: %w", err)
	}
	defer master.Close()
	defer slave.Close()
	// Raw before attach starts, so the line discipline can't eat Ctrl-C.
	if err := makeRaw(slave); err != nil {
		return fmt.Errorf("console: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, attachTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- the runtime comes from the checked config
	cmd.Stdin, cmd.Stdout = slave, slave
	var errText bytes.Buffer
	cmd.Stderr = &errText
	// SIGTERM, so sudo passes it on rather than leave attach behind.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", host.Quote(argv), err)
	}
	// attach copies the console out to the pty, and blocks once it's full.
	go func() { _, _ = io.Copy(io.Discard, master) }() // ends when master closes

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	typeErr := typeLine(ctx, exited, master, slave, line)
	if typeErr != nil && !errors.Is(typeErr, errExited) {
		_ = cmd.Cancel() // the type error is the one to show
		<-exited
		return fmt.Errorf("console: %w", typeErr)
	}
	err = <-exited
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("%s didn't detach in %s", host.Quote(argv), attachTimeout)
	case typeErr == nil && (err == nil || detachedDocker(err, errText.String())):
		return nil
	case err == nil:
		return fmt.Errorf("%s ended before it read the line", host.Quote(argv))
	}
	if first, _, _ := strings.Cut(strings.TrimSpace(errText.String()), "\n"); first != "" {
		return fmt.Errorf("%s: %w: %s", host.Quote(argv), err, first)
	}
	return fmt.Errorf("%s: %w", host.Quote(argv), err)
}

func (a AttachConsole) command() []string {
	if a.argv != nil {
		return a.argv
	}
	argv := []string{a.Runtime, "attach", "--sig-proxy=false", "--detach-keys=ctrl-c", Container}
	if a.Root {
		return argv
	}
	// sudo -v already ran on the real terminal, and the pty can't take a
	// password.
	return append([]string{"sudo", "-n"}, argv...)
}

// detachedDocker is how docker attach ends after its detach keys. Podman's
// attach exits 0.
func detachedDocker(err error, stderr string) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1 && strings.Contains(stderr, "read escape sequence")
}

// typeLine sends the line, then Ctrl-C to detach. attach only sees its detach
// keys in a read of their own, so Ctrl-C waits until the line has been read.
// The pty hands written bytes on a moment later, so an empty queue only
// counts once the line was seen in it, or lineArrive has gone by.
func typeLine(ctx context.Context, exited chan error, master, slave *os.File, line string) error {
	if _, err := io.WriteString(master, line+"\r"); err != nil {
		return err
	}
	start, seen := time.Now(), false
	for {
		n, err := queued(slave)
		if err != nil {
			return err
		}
		seen = seen || n > 0
		if n == 0 && (seen || time.Since(start) >= lineArrive) {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("attach didn't read the line")
		case err := <-exited:
			exited <- err
			return errExited
		case <-time.After(5 * time.Millisecond):
		}
	}
	_, err := master.Write([]byte{3})
	return err
}

func ioctl(f *os.File, req uint, arg unsafe.Pointer) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := conn.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(req), uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

// openPty gives a new pseudo-terminal. Neither end becomes this process's
// terminal, so sudo -n still finds the operator's own.
func openPty() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	var n uint32
	var unlock int32
	if err = ioctl(master, syscall.TIOCGPTN, unsafe.Pointer(&n)); err == nil { // #nosec G103 -- ioctl wants a pointer
		err = ioctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)) // #nosec G103
	}
	if err == nil {
		slave, err = os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|syscall.O_NOCTTY, 0)
	}
	if err != nil {
		_ = master.Close() // the open error is the one to show
		return nil, nil, err
	}
	return master, slave, nil
}

// makeRaw is cfmakeraw(3).
func makeRaw(f *os.File) error {
	var t syscall.Termios
	if err := ioctl(f, syscall.TCGETS, unsafe.Pointer(&t)); err != nil { // #nosec G103 -- ioctl wants a pointer
		return err
	}
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0
	return ioctl(f, syscall.TCSETS, unsafe.Pointer(&t)) // #nosec G103
}

// queued is how many bytes wait to be read from a terminal.
func queued(f *os.File) (int, error) {
	var n int32
	err := ioctl(f, syscall.TIOCINQ, unsafe.Pointer(&n)) // #nosec G103 -- ioctl wants a pointer
	return int(n), err
}
