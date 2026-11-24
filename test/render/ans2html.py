#!/usr/bin/env python3
"""Renders terminal output to HTML through a terminal emulator, for review screenshots."""
import html, sys
import pyte

NAMES = ['black', 'red', 'green', 'brown', 'blue', 'magenta', 'cyan', 'white']
PALETTES = {
    # LookStyled sets these slots on the console.
    'styled': ['#201F26', '#FF577D', '#00FFB2', '#3A3943', '#6B50FF', '#FF60FF', '#00A4FF', '#ECEBF0',
               '#4D4C57', '#605F6B', '#68FFD6', '#F5EF34', '#8B75FF', '#D46EFF', '#A2A0AD', '#BFBCC8'],
    # The Linux console's stock palette.
    'normal': ['#000000', '#aa0000', '#00aa00', '#aa5500', '#0000aa', '#aa00aa', '#00aaaa', '#aaaaaa',
               '#555555', '#ff5555', '#55ff55', '#ffff55', '#5555ff', '#ff55ff', '#55ffff', '#ffffff'],
}
PALETTES['true'] = PALETTES['styled']

def color(c, pal, default):
    if c == 'default':
        return default
    if c in NAMES:
        return pal[NAMES.index(c)]
    if c.startswith('bright') and c[6:] in NAMES:
        return pal[8 + NAMES.index(c[6:])]
    if len(c) == 6:
        return '#' + c
    return default

def main(path, look, cols, rows):
    pal = PALETTES[look]
    screen = pyte.Screen(cols, rows)
    pyte.ByteStream(screen).feed(open(path, 'rb').read().replace(b'\n', b'\r\n'))
    bg0, fg0 = pal[0], pal[7] if look != 'normal' else '#aaaaaa'
    out = [f'<html><body style="margin:0;background:{bg0}"><pre style="margin:0;padding:12px;'
           f'font:16px/20px \'DejaVu Sans Mono\',monospace;color:{fg0};background:{bg0}">']
    for y in range(rows):
        line = screen.buffer[y]
        for x in range(cols):
            ch = line[x]
            fg, bg = color(ch.fg, pal, fg0), color(ch.bg, pal, bg0)
            if ch.reverse:
                fg, bg = bg, fg
            w = 'bold' if ch.bold and look == 'true' else 'normal'
            out.append(f'<span style="display:inline-block;width:10px;overflow:hidden;color:{fg};background:{bg};font-weight:{w}">{html.escape(ch.data or " ")}</span>')
        out.append('\n')
    out.append('</pre></body></html>')
    sys.stdout.write(''.join(out))

main(sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4]))
