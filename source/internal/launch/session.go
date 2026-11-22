// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package launch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/host"
	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/server"
)

// consoleHello goes out before console attaches, since in both modes Ctrl-C
// detaches instead of reaching the server.
const consoleHello = "Attached to the server console. Press Ctrl-C to leave it running, or type stop to stop it."

// How long cmd waits for the answer, in polls. Paper answers most commands in
// a tick, but save-all can take seconds.
const (
	answerPoll  = 100 * time.Millisecond
	answerFirst = 30  // for the first line
	answerQuiet = 5   // after the last one
	answerMax   = 100 // in all
)

// answerChunk caps one read of the log, so a flood can't eat memory.
const answerChunk = 1 << 20

// mustRun refuses console and cmd when there's no server to talk to.
func (k *Kit) mustRun(ctx context.Context, e engine) error {
	up, err := e.running(ctx)
	if err != nil {
		return err
	}
	if !up {
		return errors.New("the server isn't running; tt-minecraftd start starts it")
	}
	return nil
}

// session hands this terminal to the console. A detach leaves the server up,
// and its trees with it. An exit, such as after stop, hands them back.
// detached, when set, says whether an error from c and the end of its stderr
// are only how it detaches. The exit code alone can't say, since sudo and
// docker exit 1 on their own failures too.
func (k *Kit) session(ctx context.Context, e engine, c host.Cmd, detached func(err error, stderr string) bool) error {
	var said errTail
	if detached != nil {
		c.ErrCopy = &said
	}
	_, err := k.Run.Run(ctx, c)
	if k.DryRun {
		return err
	}
	up, upErr := e.running(ctx)
	if upErr == nil && up {
		if err != nil && detached != nil && detached(err, string(said.buf)) {
			err = nil
		}
		if err == nil {
			k.say("Detached. The server is still running; stop stops it.")
		}
		return err
	}
	if upErr != nil {
		return errors.Join(err, upErr)
	}
	k.say("The server stopped.")
	return errors.Join(err, k.handBack(ctx))
}

// errTailMax is how much of a console's stderr errTail keeps. docker attach
// says it detached in its last line.
const errTailMax = 4096

// errTail keeps the end of what's written to it.
type errTail struct {
	buf []byte
}

func (t *errTail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > errTailMax {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-errTailMax:]...)
	}
	return len(p), nil
}

// checkCmd keeps stop and restart off cmd, since stop also hands the trees
// back, and Paper's restart only stops without a restart script. A namespace
// such as minecraft: or bukkit: names the same command, and execute and
// return run one after their run keyword, nested or not.
func checkCmd(line string) error {
	words := strings.Fields(line)
	if len(words) == 0 {
		return errors.New("no console command given")
	}
	first := cmdName(words[0])
	if first == "stop" || first == "restart" {
		return stopRefused(words[0])
	}
	// Every word after a run is tried as the command, which covers a nested
	// execute too, and is linear. A player or score named run can only refuse
	// more.
	if first == "execute" || first == "return" {
		for i := 1; i < len(words)-1; i++ {
			if strings.ToLower(words[i]) != "run" {
				continue
			}
			if name := cmdName(words[i+1]); name == "stop" || name == "restart" {
				return stopRefused(words[i+1])
			}
		}
	}
	return nil
}

func stopRefused(word string) error {
	return fmt.Errorf("%s isn't sent through cmd; tt-minecraftd stop stops the server and hands the trees back", word)
}

// cmdName gives a console command's name without its slash or namespace,
// lower-cased, since Bukkit's names ignore case.
func cmdName(word string) string {
	name := strings.ToLower(strings.TrimPrefix(word, "/"))
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// command sends one console line and prints the log lines that follow it,
// until they stop coming. See design.md, console and cmd.
func (k *Kit) command(ctx context.Context, e engine, line string) error {
	if err := checkCmd(line); err != nil {
		return err
	}
	if err := k.mustRun(ctx, e); err != nil {
		return err
	}
	// Checked before the line goes out, so it isn't sent with no answer shown.
	if err := k.groupProblem("the server's log"); err != nil {
		return err
	}
	log := &logFollow{serverDir: k.Cfg.ServerDir}
	defer log.close()
	if err := log.skip(); err != nil {
		return err
	}
	if err := k.Console.Send(ctx, line); err != nil {
		return err
	}
	if k.DryRun {
		return nil
	}
	// Only the server thread's lines count, since the answer comes from there.
	// Geyser's STUN lines right after a start used to end the wait before it.
	// A line with no thread goes with the one before it.
	seen, quiet, ours := false, 0, true
	for i := range answerMax {
		if err := k.pause(ctx, answerPoll); err != nil {
			return fmt.Errorf("%w after the line was sent", err)
		}
		lines, err := log.next()
		if err != nil {
			return err
		}
		heard := false
		for _, l := range lines {
			k.say("%s", l)
			if thread, ok := server.LogThread(l); ok {
				ours = thread == server.MainThread
			}
			heard = heard || ours
		}
		switch {
		case heard:
			seen, quiet = true, 0
		case seen:
			quiet++
		}
		if (seen && quiet >= answerQuiet) || (!seen && i+1 >= answerFirst) {
			break
		}
	}
	return nil
}

// logFollow reads what's added to a log. A new or shorter file, as Paper
// starts each day, is read from its start.
type logFollow struct {
	serverDir string
	// last is the file read last time, held open so its inode can't go to
	// the next new file, as ext4 does once a removed file is closed.
	last *os.File
	off  int64
	// part is a line that isn't finished yet.
	part []byte
	// cut is set when skip stopped inside a line, which belongs to before.
	cut bool
}

// skip moves to the end of the file, so only what comes next is read.
func (f *logFollow) skip() error {
	fh, err := server.OpenLog(f.serverDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f.last = fh
	fi, err := fh.Stat()
	if err != nil {
		return err
	}
	f.off = fi.Size()
	if f.off > 0 {
		end := make([]byte, 1)
		if _, err := fh.ReadAt(end, f.off-1); err != nil {
			return err
		}
		f.cut = end[0] != '\n'
	}
	return nil
}

func (f *logFollow) next() ([]string, error) {
	fh, err := server.OpenLog(f.serverDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var last os.FileInfo
	if f.last != nil {
		last, _ = f.last.Stat() // nil reads it again from the start
	}
	f.close()
	f.last = fh
	fi, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if last == nil || !os.SameFile(last, fi) || fi.Size() < f.off {
		f.off, f.part, f.cut = 0, nil, false
	}
	buf := make([]byte, min(fi.Size()-f.off, answerChunk))
	n, err := fh.ReadAt(buf, f.off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	f.off += int64(n)
	data := append(f.part, buf[:n]...)
	if f.cut {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return nil, nil
		}
		data, f.cut = data[i+1:], false
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		f.part = data
		return nil, nil
	}
	f.part = append([]byte(nil), data[end+1:]...)
	lines := strings.Split(string(data[:end]), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, "\r")
	}
	return lines, nil
}

func (f *logFollow) close() {
	if f.last != nil {
		_ = f.last.Close() // only read from
		f.last = nil
	}
}
