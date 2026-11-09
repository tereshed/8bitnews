import struct
import sys
import zlib
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
from pngshrink import Unsupported, halve, read_png, thumbnail, write_png  # noqa: E402


def filtered_png(path, width, rows, kinds, channels=3):
    """A PNG whose rows use the given filter types, encoded the way PNG writers do."""
    color = {3: 2, 4: 6}[channels]

    def paeth(a, b, c):
        p = a + b - c
        pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
        return a if pa <= pb and pa <= pc else b if pb <= pc else c

    raw, previous = b"", bytes(width * channels)
    for y, row in enumerate(rows):
        kind = kinds[y % len(kinds)]
        encoded = bytearray(len(row))
        for i, value in enumerate(row):
            left = row[i - channels] if i >= channels else 0
            up = previous[i]
            up_left = previous[i - channels] if i >= channels else 0
            encoded[i] = (value - {0: 0, 1: left, 2: up, 3: (left + up) // 2, 4: paeth(left, up, up_left)}[kind]) & 255
        raw += bytes([kind]) + bytes(encoded)
        previous = row

    def chunk(name, body):
        return struct.pack(">I", len(body)) + name + body + struct.pack(">I", zlib.crc32(name + body))

    Path(path).write_bytes(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, len(rows), 8, color, 0, 0, 0))
                           + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


def blocks(width, height, size=2):
    """RGB rows where every size×size block has its own color."""
    rows = []
    for y in range(height):
        row = bytearray()
        for x in range(width):
            row += bytes(((x // size) * 37 % 256, (y // size) * 53 % 256, (x // size + y // size) * 11 % 256))
        rows.append(bytes(row))
    return rows


@pytest.mark.parametrize("kinds", [[0], [1], [2], [3], [4], [0, 1, 2, 3, 4]])
def test_reads_every_filter_type(tmp_path, kinds):
    rows = blocks(8, 10, 1)
    filtered_png(tmp_path / "a.png", 8, rows, kinds)
    assert read_png(tmp_path / "a.png") == (8, 10, 2, rows)


@pytest.mark.parametrize("channels", [3, 4])
def test_halve_keeps_every_pixel_of_the_grid(tmp_path, channels):
    rows = blocks(8, 6) if channels == 3 else [bytes(v for i in range(0, len(r), 3) for v in (*r[i:i + 3], 200))
                                               for r in blocks(8, 6)]
    filtered_png(tmp_path / "a.png", 8, rows, [4, 1, 2], channels)
    assert halve(tmp_path / "a.png") is True
    width, height, _, small = read_png(tmp_path / "a.png")
    assert (width, height) == (4, 3)
    for y in range(6):
        for x in range(8):
            assert rows[y][x * channels:(x + 1) * channels] == small[y // 2][(x // 2) * channels:(x // 2 + 1) * channels]


@pytest.mark.parametrize("edit", ["row", "column"])
def test_halve_refuses_a_frame_off_the_grid(tmp_path, edit):
    rows = blocks(8, 6)
    changed = bytearray(rows[1])
    changed[3 if edit == "column" else 0] ^= 1  # second pixel of a block, or the second row of a block
    rows[1 if edit == "row" else 0] = bytes(changed) if edit == "row" else rows[0]
    if edit == "column":
        first = bytearray(rows[0])
        first[3] ^= 1
        rows[0] = rows[1] = bytes(first)
    filtered_png(tmp_path / "a.png", 8, rows, [0])
    before = (tmp_path / "a.png").read_bytes()
    assert halve(tmp_path / "a.png") is False
    assert (tmp_path / "a.png").read_bytes() == before


def test_halve_refuses_odd_sizes(tmp_path):
    filtered_png(tmp_path / "a.png", 3, blocks(3, 4, 1), [0])
    assert halve(tmp_path / "a.png") is False


def test_unsupported_png_is_an_error_not_a_guess(tmp_path):
    (tmp_path / "a.png").write_bytes(b"not a png")
    with pytest.raises(Unsupported):
        read_png(tmp_path / "a.png")


@pytest.mark.parametrize("body", [
    b"\x89PNG\r\n\x1a\n\x00\x00\x00\x02IHDR\x00\x00\x00\x00\x00\x00",  # short header
    b"\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x00\x00\x00\x00"
    b"\x00\x00\x00\x01IDATx\x00\x00\x00\x00",  # broken data
])
def test_a_malformed_png_is_unsupported_not_a_crash(tmp_path, body):
    (tmp_path / "a.png").write_bytes(body)
    with pytest.raises(Unsupported):
        read_png(tmp_path / "a.png")


def test_thumbnail_averages_blocks_and_keeps_proportions(tmp_path):
    rows = [bytes((0, 0, 0, 200, 100, 50)) * 4 for _ in range(16)]  # alternating columns average to half
    filtered_png(tmp_path / "a.png", 8, rows, [0])
    thumbnail(tmp_path / "a.png", tmp_path / "t.png", width=4)
    width, height, color, small = read_png(tmp_path / "t.png")
    assert (width, height, color) == (4, 8, 2)
    assert small[0][:3] == bytes((100, 50, 25))


def test_write_png_round_trip(tmp_path):
    rows = blocks(4, 4, 1)
    write_png(tmp_path / "a.png", 4, 4, 2, rows)
    assert read_png(tmp_path / "a.png") == (4, 4, 2, rows)
