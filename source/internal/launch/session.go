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
// detached, when set, says whether an error from c is only how it detaches.
func (k *Kit) session(ctx context.Context, e engine, c host.Cmd, detached func(error) bool) error {
	_, err := k.Run.Run(ctx, c)
	if k.DryRun {
		return err
	}
	up, upErr := e.running(ctx)
	if upErr == nil && up {
		if err != nil && detached != nil && detached(err) {
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

// checkCmd keeps stop and restart off cmd, since stop also hands the trees
// back, and Paper's restart only stops without a restart script.
func checkCmd(line string) error {
	word, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	switch strings.ToLower(strings.TrimPrefix(word, "/")) {
	case "":
		return errors.New("no console command given")
	case "stop", "restart":
		return fmt.Errorf("%s isn't sent through cmd; tt-minecraftd stop stops the server and hands the trees back", word)
	}
	return nil
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
	log := &logFollow{file: server.LogFile(k.Cfg.ServerDir)}
	if err := log.skip(); err != nil {
		return err
	}
	if err := k.Console.Send(ctx, line); err != nil {
		return err
	}
	if k.DryRun {
		return nil
	}
	seen, quiet := false, 0
	for i := range answerMax {
		if ctx.Err() != nil {
			return nil
		}
		k.Sleep(answerPoll)
		lines, err := log.next()
		if err != nil {
			return err
		}
		for _, l := range lines {
			k.say("%s", server.Clean(l))
		}
		switch {
		case len(lines) > 0:
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
	file string
	last os.FileInfo
	off  int64
	// part is a line that isn't finished yet.
	part []byte
	// cut is set when skip stopped inside a line, which belongs to before.
	cut bool
}

// skip moves to the end of the file, so only what comes next is read.
func (f *logFollow) skip() error {
	fh, err := server.OpenRegular(f.file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return err
	}
	f.last, f.off = fi, fi.Size()
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
	fh, err := server.OpenRegular(f.file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if f.last == nil || !os.SameFile(f.last, fi) || fi.Size() < f.off {
		f.off, f.part, f.cut = 0, nil, false
	}
	f.last = fi
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
