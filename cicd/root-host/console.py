#!/usr/bin/env python3
# Types one line into tt-minecraftd console on a pty, then Ctrl-C, the way a
# person would. Exits with console's own exit code.
# Usage: console.py LINE
import os
import pty
import select
import sys
import time

line = sys.argv[1]
os.environ.setdefault("TERM", "xterm")  # tmux won't attach without one
pid, fd = pty.fork()
if pid == 0:
    os.execvp("tt-minecraftd", ["tt-minecraftd", "console"])

seen = b""


def pump(secs):
    global seen
    end = time.time() + secs
    while time.time() < end:
        ready, _, _ = select.select([fd], [], [], 0.2)
        if not ready:
            continue
        try:
            got = os.read(fd, 4096)
        except OSError:
            return
        if not got:
            return
        seen += got


pump(4)
os.write(fd, line.encode() + b"\r")
pump(3)
os.write(fd, b"\x03")
pump(5)
done, status = os.waitpid(pid, os.WNOHANG)
print(seen.decode(errors="replace")[-400:])
if done == 0:
    print("console didn't leave on Ctrl-C")
    os.kill(pid, 9)
    sys.exit(1)
sys.exit(os.waitstatus_to_exitcode(status))
