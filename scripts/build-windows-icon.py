#!/usr/bin/env python3
"""Regenerate the checked-in Windows ICO from the existing Aide native mark.

Maintainer-only macOS tool (AppKit/CoreText and sips, no third-party packages).
Windows/Linux builds consume the checked-in ICO and need no icon renderer.
"""
from pathlib import Path
import platform
import struct
import subprocess
import tempfile
import zlib
ROOT = Path(__file__).resolve().parents[1]
SIZES = (16, 20, 24, 32, 40, 48, 64, 96, 128, 256)

def bitmap_frame(png):
    """Losslessly store a generated RGBA PNG as the conventional ICO DIB/mask."""
    if png[:8] != b'\x89PNG\r\n\x1a\n':
        raise ValueError('Expected generated PNG')
    width, height, bits, color, compression, filtering, interlace = struct.unpack_from('>IIBBBBB', png, 16)
    if width != height or not 1 <= width <= 256 or (bits, color, compression, filtering, interlace) != (8, 6, 0, 0, 0):
        raise ValueError('Expected bounded square non-interlaced RGBA8 PNG')
    offset, compressed = 8, bytearray()
    while offset < len(png):
        length = struct.unpack_from('>I', png, offset)[0]
        kind = png[offset+4:offset+8]
        if kind == b'IDAT': compressed += png[offset+8:offset+8+length]
        offset += length + 12
    raw = zlib.decompress(compressed); stride = width * 4
    if len(raw) != height * (stride + 1): raise ValueError('Unexpected PNG row data')
    previous = bytearray(stride); rows = []
    for y in range(height):
        start = y * (stride+1); mode = raw[start]; row = bytearray(raw[start+1:start+1+stride])
        if mode > 4: raise ValueError('Invalid PNG filter')
        for x in range(stride):
            left = row[x-4] if x >= 4 else 0; up = previous[x]; diagonal = previous[x-4] if x >= 4 else 0
            predicted = left + up - diagonal
            distances = [abs(predicted-v) for v in (left, up, diagonal)]
            paeth = (left, up, diagonal)[distances.index(min(distances))]
            row[x] = (row[x] + (0, left, up, (left+up)//2, paeth)[mode]) % 256
        rows.append(row); previous = row
    pixels, mask = bytearray(), bytearray()
    for row in reversed(rows):
        line = bytearray(((width+31)//32)*4)
        for x in range(width):
            r,g,b,a = row[4*x:4*x+4]; pixels += bytes((b,g,r,a))
            if not a: line[x//8] |= 0x80 >> (x%8)
        mask += line
    return struct.pack('<IiiHHIIiiII',40,width,height*2,1,32,0,0,0,0,0,0) + pixels + mask

def main():
    if platform.system() != 'Darwin':
        raise SystemExit('Regeneration requires macOS; normal builds use the checked-in ICO')
    with tempfile.TemporaryDirectory(prefix='aide-windows-icon-') as temporary:
        root = Path(temporary)
        renderer = root / 'render-icon'
        images = root / 'images'
        subprocess.run(['xcrun', 'swiftc', '-module-cache-path', str(root / 'modules'),
                        str(ROOT / 'scripts/render-computer-bridge-icon.swift'), '-o', str(renderer)], check=True)
        subprocess.run([str(renderer), str(images)], check=True)
        originals = {16: 'icon_16x16.png', 32: 'icon_32x32.png', 64: 'icon_32x32@2x.png',
                     128: 'icon_128x128.png', 256: 'icon_128x128@2x.png'}
        frames = []
        for size in SIZES:
            path = images / originals.get(size, f'windows-{size}.png')
            if size not in originals:
                subprocess.run(['sips', '-z', str(size), str(size), str(images / originals[256]),
                                '--out', str(path)], check=True, stdout=subprocess.DEVNULL)
            data = path.read_bytes()
            frames.append((size, data if size == 256 else bitmap_frame(data)))
        result = bytearray(struct.pack('<HHH', 0, 1, len(frames)))
        offset = 6 + 16 * len(frames)
        for size, data in frames:
            result += struct.pack('<BBBBHHII', size % 256, size % 256, 0, 0, 1, 32, len(data), offset)
            offset += len(data)
        result += b''.join(data for _, data in frames)
        output = ROOT / 'native/windows-launcher/resources/aide.ico'
        output.write_bytes(result)
        print(f'Generated {output}: {len(frames)} DIB/PNG frames, {len(result)} bytes')
if __name__ == '__main__':
    main()
