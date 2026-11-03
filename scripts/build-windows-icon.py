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
ROOT = Path(__file__).resolve().parents[1]
SIZES = (16, 20, 24, 32, 40, 48, 64, 96, 128, 256)

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
            frames.append((size, path.read_bytes()))
        result = bytearray(struct.pack('<HHH', 0, 1, len(frames)))
        offset = 6 + 16 * len(frames)
        for size, data in frames:
            result += struct.pack('<BBBBHHII', size % 256, size % 256, 0, 0, 1, 32, len(data), offset)
            offset += len(data)
        result += b''.join(data for _, data in frames)
        output = ROOT / 'native/windows-launcher/resources/aide.ico'
        output.write_bytes(result)
        print(f'Generated {output}: {len(frames)} PNG frames, {len(result)} bytes')
if __name__ == '__main__':
    main()
