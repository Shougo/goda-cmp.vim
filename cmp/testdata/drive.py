#!/usr/bin/env python3
"""Drive a real Vim in a pseudo terminal with a list of key strings.

usage: drive.py VIM VIMRC FILE STEPS_JSON

STEPS_JSON is a list of [keys, seconds_to_wait]. The pty is drained all the
time so that Vim never blocks on output. Exit status 0 when Vim quit by itself.
"""
import fcntl, json, os, pty, select, struct, sys, termios, time


def main():
    vim, vimrc, path, steps = sys.argv[1], sys.argv[2], sys.argv[3], json.loads(sys.argv[4])
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.execvp(vim, [vim, "-u", vimrc, "-i", "NONE", "-N", path])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))

    def drain(seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.05)
            if r:
                try:
                    if not os.read(fd, 65536):
                        return False
                except OSError:
                    return False
        return True

    alive = drain(1.5)  # startup, plugin start
    for keys, wait in steps:
        if not alive:
            break
        os.write(fd, keys.encode())
        alive = drain(wait)
    if alive:
        os.write(fd, b"\x1b:qa!\r")
        drain(1.0)
    try:
        os.kill(pid, 9)
    except OSError:
        pass
    os.waitpid(pid, 0)


main()
