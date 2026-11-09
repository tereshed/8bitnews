#!/usr/bin/env python3
"""Shrink review images with the standard library: agent sandboxes have no Pillow or ffmpeg.

Every frame the studio renders is a 270×480 grid scaled up without smoothing, so halving it loses no pixel of the
picture and costs a reviewer a quarter of the image tokens. `halve` proves that on each frame and refuses otherwise.
"""
from __future__ import annotations

import struct
import zlib
from pathlib import Path

SIGNATURE = b"\x89PNG\r\n\x1a\n"
CHANNELS = {2: 3, 6: 4}  # RGB and RGBA; the studio writes nothing else


class Unsupported(ValueError):
    """A PNG this module does not read: the caller keeps the original."""


def _paeth(a: int, b: int, c: int) -> int:
    p = a + b - c
    pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
    if pa <= pb and pa <= pc:
        return a
    return b if pb <= pc else c


def _unfilter(kind: int, line: bytes, previous: bytes, channels: int) -> bytes:
    if kind == 0:
        return line
    if not any(line):  # a run of equal pixels or rows, the usual case in pixel art
        if kind == 2:
            return previous
        if kind == 1:
            return line
    out = bytearray(line)
    if kind == 1:
        for i in range(channels, len(out)):
            out[i] = (out[i] + out[i - channels]) & 255
    elif kind == 2:
        for i in range(len(out)):
            out[i] = (out[i] + previous[i]) & 255
    elif kind == 3:
        for i in range(len(out)):
            left = out[i - channels] if i >= channels else 0
            out[i] = (out[i] + (left + previous[i]) // 2) & 255
    elif kind == 4:
        for i in range(len(out)):
            left = out[i - channels] if i >= channels else 0
            up_left = previous[i - channels] if i >= channels else 0
            out[i] = (out[i] + _paeth(left, previous[i], up_left)) & 255
    else:
        raise Unsupported(f"unknown PNG filter {kind}")
    return bytes(out)


def read_png(path: Path) -> tuple[int, int, int, list[bytes]]:
    """Return width, height, color type and the unfiltered rows of an 8-bit RGB or RGBA PNG."""
    data = Path(path).read_bytes()
    if not data.startswith(SIGNATURE):
        raise Unsupported("not a PNG")
    pos, idat, header = len(SIGNATURE), [], None
    while pos + 8 <= len(data):
        length, name = struct.unpack(">I4s", data[pos:pos + 8])
        chunk = data[pos + 8:pos + 8 + length]
        if name == b"IHDR":
            if len(chunk) != 13:
                raise Unsupported("malformed PNG header")
            header = struct.unpack(">IIBBBBB", chunk)
        elif name == b"IDAT":
            idat.append(chunk)
        elif name == b"IEND":
            break
        pos += 12 + length
    if not header or not idat:
        raise Unsupported("PNG without IHDR or IDAT")
    width, height, depth, color, _, _, interlace = header
    if depth != 8 or color not in CHANNELS or interlace:
        raise Unsupported(f"PNG depth {depth}, color {color}, interlace {interlace}")
    channels = CHANNELS[color]
    stride = width * channels
    try:
        raw = zlib.decompress(b"".join(idat))
    except zlib.error as err:
        raise Unsupported(f"broken PNG data: {err}") from err
    if len(raw) != height * (stride + 1):
        raise Unsupported("PNG data size does not match its header")
    rows, previous = [], bytes(stride)
    for y in range(height):
        start = y * (stride + 1)
        previous = _unfilter(raw[start], raw[start + 1:start + 1 + stride], previous, channels)
        rows.append(previous)
    return width, height, color, rows


def write_png(path: Path, width: int, height: int, color: int, rows: list[bytes]) -> None:
    def chunk(name: bytes, body: bytes) -> bytes:
        return struct.pack(">I", len(body)) + name + body + struct.pack(">I", zlib.crc32(name + body))

    raw = b"".join(b"\0" + row for row in rows)
    Path(path).write_bytes(SIGNATURE + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, color, 0, 0, 0))
                           + chunk(b"IDAT", zlib.compress(raw, 6)) + chunk(b"IEND", b""))


def halve(path: Path) -> bool:
    """Halve a PNG in place if every 2×2 block is one color. False and untouched if halving would lose anything."""
    width, height, color, rows = read_png(path)
    if width % 2 or height % 2:
        return False
    channels = CHANNELS[color]
    step = channels * 2
    out = []
    for y in range(0, height, 2):
        row = rows[y]
        if rows[y + 1] != row:
            return False
        small = bytearray(width // 2 * channels)
        for c in range(channels):
            if row[c::step] != row[c + channels::step]:
                return False
            small[c::channels] = row[c::step]
        out.append(bytes(small))
    write_png(path, width // 2, height // 2, color, out)
    return True


def thumbnail(source: Path, target: Path, width: int = 160) -> None:
    """Write an RGB copy `width` px wide, averaging the pixels of each block as a small preview does."""
    src_width, src_height, color, rows = read_png(source)
    channels = CHANNELS[color]
    height = max(1, round(src_height * width / src_width))
    out = []
    for y in range(height):
        y0 = y * src_height // height
        y1 = max(y0 + 1, (y + 1) * src_height // height)
        row = bytearray(width * 3)
        for x in range(width):
            x0 = x * src_width // width
            x1 = max(x0 + 1, (x + 1) * src_width // width)
            count = (y1 - y0) * (x1 - x0)
            for c in range(3):
                total = sum(sum(rows[yy][x0 * channels + c:x1 * channels:channels]) for yy in range(y0, y1))
                row[x * 3 + c] = total // count
        out.append(bytes(row))
    write_png(target, width, height, 2, out)
