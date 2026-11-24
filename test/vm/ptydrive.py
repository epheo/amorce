#!/usr/bin/env python3
"""Runs a command in a pty and follows a script from stdin, one step a line:
  wait REGEX SECONDS   until the rendered screen matches
  keys WORD...         key names (enter, f2, ctrl+u...) or text to type
  screen               print the screen
It exits 1 when a wait times out, after printing the screen."""
import os, pty, re, select, sys, time

import pyte

KEYS = {
    'enter': b'\r', 'tab': b'\t', 'esc': b'\x1b', 'bs': b'\x7f', 'space': b' ',
    'up': b'\x1b[A', 'down': b'\x1b[B', 'right': b'\x1b[C', 'left': b'\x1b[D',
    'f2': b'\x1bOQ', 'f3': b'\x1bOR', 'f4': b'\x1bOS', 'ctrl+u': b'\x15',
}

def main():
    cols, rows = 100, 30
    cmd = sys.argv[1:]
    script = sys.stdin.read().splitlines()
    screen = pyte.Screen(cols, rows)
    stream = pyte.ByteStream(screen)
    pid, fd = pty.fork()
    if pid == 0:
        os.environ['TERM'] = 'xterm-256color'
        os.execvp(cmd[0], cmd)
    import fcntl, struct, termios
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))

    def pump(t):
        end = time.time() + t
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.1)
            if r:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    return False
                if not data:
                    return False
                stream.feed(data)
        return True

    def text():
        return '\n'.join(screen.display)

    for line in script:
        f = line.split()
        if not f:
            continue
        if f[0] == 'wait':
            pat, secs = re.compile(' '.join(f[1:-1])), float(f[-1])
            end = time.time() + secs
            while not pat.search(text()):
                if time.time() > end or not pump(0.5):
                    print(f'timeout waiting for: {pat.pattern}')
                    print(text())
                    sys.exit(1)
        elif f[0] == 'keys':
            for w in f[1:]:
                os.write(fd, KEYS.get(w, w.encode()))
                pump(0.2)
        elif f[0] == 'screen':
            pump(0.5)
            print(text())
    os.kill(pid, 9)

main()
