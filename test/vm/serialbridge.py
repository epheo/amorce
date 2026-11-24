#!/usr/bin/env python3
"""Holds one connection to a VM serial console: logs it, renders it with pyte, types into it."""
import argparse, os, select, socket, sys, time

KEYS = {
    'enter': b'\r', 'tab': b'\t', 'esc': b'\x1b', 'bs': b'\x7f', 'space': b' ',
    'up': b'\x1b[A', 'down': b'\x1b[B', 'right': b'\x1b[C', 'left': b'\x1b[D',
    'f2': b'\x1bOQ', 'f3': b'\x1bOR', 'f5': b'\x1b[15~', 'f10': b'\x1b[21~', 'f12': b'\x1b[24~',
    'ctrl+c': b'\x03', 'ctrl+s': b'\x13', 'ctrl+d': b'\x04', 'ctrl+l': b'\x0c',
}

def send(fifo, words):
    out = b''
    for w in words:
        out += KEYS.get(w, w.encode())
    fd = os.open(fifo, os.O_WRONLY)
    os.write(fd, out)
    os.close(fd)

def serve(a):
    import pyte
    os.makedirs(a.dir, exist_ok=True)
    fifo = os.path.join(a.dir, 'in')
    if not os.path.exists(fifo):
        os.mkfifo(fifo)
    screen = pyte.Screen(a.cols, a.rows)
    stream = pyte.ByteStream(screen)
    log = open(os.path.join(a.dir, 'serial.log'), 'ab', buffering=0)

    def connect():
        while True:
            try:
                s = socket.create_connection(('127.0.0.1', a.port), timeout=5)
                s.setblocking(False)
                return s
            except OSError:
                time.sleep(1)

    def dump():
        tmp = os.path.join(a.dir, 'screen.tmp')
        with open(tmp, 'w') as f:
            f.write('\n'.join(screen.display) + '\n')
        os.replace(tmp, os.path.join(a.dir, 'screen.txt'))

    s = connect()
    # Answer the terminal's own reports (cursor position, size probes) like a real terminal.
    holder = {'s': s}
    def reply(data):
        try:
            holder['s'].setblocking(True)
            holder['s'].sendall(data.encode())
            holder['s'].setblocking(False)
        except OSError:
            pass
    screen.write_process_input = reply
    rfd = os.open(fifo, os.O_RDONLY | os.O_NONBLOCK)
    wfd = os.open(fifo, os.O_WRONLY)  # keeps the fifo open between writers
    while True:
        r, _, _ = select.select([s, rfd], [], [], 1.0)
        if s in r:
            try:
                data = s.recv(65536)
            except (BlockingIOError, ConnectionResetError):
                data = b''
            if not data:
                s.close()
                s = connect()
                holder['s'] = s
                continue
            log.write(data)
            stream.feed(data)
            dump()
        if rfd in r:
            data = os.read(rfd, 4096)
            if data:
                s.setblocking(True)
                s.sendall(data)
                s.setblocking(False)

ap = argparse.ArgumentParser()
ap.add_argument('--port', type=int)
ap.add_argument('--dir')
ap.add_argument('--cols', type=int, default=80)
ap.add_argument('--rows', type=int, default=24)
ap.add_argument('--send')
a, rest = ap.parse_known_args()
if a.send:
    send(a.send, rest)
else:
    serve(a)
