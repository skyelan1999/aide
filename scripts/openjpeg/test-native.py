#!/usr/bin/env python3
"""Pinned native ASan/UBSan/LSan checks with two explicit realloc fault sites.

Requires the verified compiler image from pins.json. Uses only disposable files;
the product artifact contains neither the fault hook nor the HT capture hook.
"""
import argparse
import base64
import urllib.request
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location('openjpeg_build', HERE / 'build.py')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


def command(work, compiler, text):
    subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--entrypoint', 'sh',
                    '-v', str(work / 'core') + ':/core', '-v', str(work / 'fixtures') + ':/fixtures',
                    '-v', str(HERE) + ':/tests:ro', compiler, '-ec', text], check=True)


def tile_variant(data, outer):
    cod = data.index(b'\xff\x52')
    count = int.from_bytes(data[cod + 2:cod + 4], 'big') + 2
    coding = bytearray(data[cod:cod + count])
    if outer:
        coding[4] |= 1
        coding += bytes([0, 0x11, 0x11])
        coding[2:4] = (len(coding) - 2).to_bytes(2, 'big')
    else:
        coding[10] = coding[11] = 0  # 4x4 blocks instead of 16x16
    first = data.index(b'\xff\x90')
    second = first + int.from_bytes(data[first + 6:first + 10], 'big')
    if data[second:second + 2] != b'\xff\x90':
        raise ValueError('Expected two encoder-generated tiles')
    result = bytearray(data[:second + 12] + coding + data[second + 12:])
    result[second + 6:second + 10] = (int.from_bytes(data[second + 6:second + 10], 'big') + len(coding)).to_bytes(4, 'big')
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--fixtures', type=Path, help='optional retained reported/official fixture directory')
    args = parser.parse_args()
    pins = json.loads((HERE / 'pins.json').read_text())
    work = args.work.resolve()
    work.mkdir(parents=True, exist_ok=True)
    core, fixtures = work / 'core', work / 'fixtures'
    fixtures.mkdir()
    catalog = json.loads((ROOT / 'scripts/fixtures/pdf-jpx-native-fixes.json').read_text())
    for row in catalog['images']:
        data = base64.b64decode(row['base64'], validate=True)
        if hashlib.sha256(data).hexdigest() != row['sha256']:
            raise ValueError('Fixture hash mismatch: ' + row['name'])
        (fixtures / row['name']).write_bytes(data)
    vectors = json.loads((HERE / 'official-vectors.json').read_text())
    for row in vectors:
        retained = args.fixtures / row['name'] if args.fixtures else None
        data = retained.read_bytes() if retained and retained.is_file() else urllib.request.urlopen(row['url'], timeout=60).read(2 << 20)
        if hashlib.sha256(data).hexdigest() != row['sha256']:
            raise ValueError('Official vector hash mismatch: ' + row['name'])
        (fixtures / row['name']).write_bytes(data)
    subprocess.run(['node', 'scripts/pdf-jpx-packet-borrow.test.cjs'], cwd=ROOT,
                   env={**os.environ, 'AIDE_PACKET_FIXTURE_DIR': str(fixtures)}, check=True)
    patch = HERE / 'native-fixes.patch'
    if builder.digest(patch) != pins['core']['patchSHA256']:
        raise ValueError('Native source patch changed')
    builder.source(pins['core'], core, work)
    subprocess.run(['git', 'init', '-q', str(core)], check=True)
    subprocess.run(['git', 'apply', str(patch)], cwd=core, check=True)
    # Real encoder input; deterministic two-tile noise keeps nested decoder
    # allocations populated before the second tile grows either owner array.
    pixels = bytes((i * 73 + (i >> 7) * 17) & 255 for i in range(256 * 128))
    (fixtures / 'growth.pgm').write_bytes(b'P5\n256 128\n255\n' + pixels)
    (fixtures / 'one.pgm').write_bytes(b'P5\n1 1\n255\n\x80')
    command(work, pins['compiler'],
            'cmake -S /core -B /core/native -DBUILD_SHARED_LIBS=OFF -DOPJ_USE_THREAD=OFF '
            '-DCMAKE_BUILD_TYPE=Release -DCMAKE_C_FLAGS="-O1 -g -DNDEBUG -fsanitize=address,undefined -fno-omit-frame-pointer" '
            '-DCMAKE_EXE_LINKER_FLAGS="-fsanitize=address,undefined" && cmake --build /core/native -j4 && '
            '/core/native/bin/opj_compress -i /fixtures/growth.pgm -o /fixtures/growth.j2k -t 128,128 -b 16,16 -n 3 && '
            '/core/native/bin/opj_compress -i /fixtures/one.pgm -o /fixtures/one.j2k -n 1')
    data = (fixtures / 'growth.j2k').read_bytes()
    (fixtures / 'growth-inner.j2k').write_bytes(tile_variant(data, False))
    (fixtures / 'growth-outer.j2k').write_bytes(tile_variant(data, True))
    # Insert only test hooks at reviewed source sites, preserving the return-NULL
    # semantics of realloc. The production source patch has no test hooks.
    file = core / 'src/lib/openjp2/tcd.c'
    text = file.read_text()
    text = text.replace('#include "opj_includes.h"', '#include "opj_includes.h"\nextern void *aide_fault_realloc(void *, size_t, unsigned);')
    for site, pointer in [(1, 'l_band->precincts'), (2, 'l_current_precinct->cblks.blocks')]:
        import re
        pattern = r'opj_realloc\(\s*' + re.escape(pointer) + r',(/\*3 \* \*/)?\s*([a-z_]+)\)'
        text, count = re.subn(pattern, lambda m: 'aide_fault_realloc(' + pointer + ', ' + m[2] + ', ' + str(site) + ')', text)
        if count != 1:
            raise ValueError('Fault-site layout changed: ' + pointer)
    file.write_text(text)
    file = core / 'src/lib/openjp2/ht_dec.c'
    text = file.read_text().replace('#include "opj_includes.h"', '#include "opj_includes.h"\nextern void aide_capture_ht(opj_tcd_cblk_dec_t *, unsigned, unsigned);')
    marker = '    (void)(orient);      // stops unused parameter message'
    if text.count(marker) != 1:
        raise ValueError('HT entry layout changed')
    file.write_text(text.replace(marker, '    aide_capture_ht(cblk, cblksty, orient);\n' + marker))
    command(work, pins['compiler'],
            'cmake --build /core/native --target openjp2 -j4 && '
            'cc -O1 -g -DNDEBUG -fsanitize=address,undefined -fno-omit-frame-pointer '
            '-I/core/src/lib/openjp2 -I/core/native/src/lib/openjp2 '
            '/tests/native-regression.c /core/native/bin/libopenjp2.a -lm -lpthread -o /core/native-harness && '
            'export ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 && '
            '/core/native-harness decode /fixtures/file1_b11.jph /fixtures/file1_b12.jph '
            '/fixtures/653-reproducer.j2k /fixtures/625-offset-100.j2k /fixtures/625-offset-100p75.j2k '
            '/fixtures/623-second-selected-mcc-index4.j2k /fixtures/642-no-segsym-control.j2k && '
            '/core/native-harness reject /fixtures/642-reported.j2k && '
            '/core/native-harness decode /fixtures/ppm.j2k /fixtures/ppt.j2k && '
            '/core/native-harness file /fixtures/ppm.j2k /fixtures/ppt.j2k && '
            '/core/native-harness reject /fixtures/ppm-duplicate.j2k /fixtures/ppt-duplicate.j2k && '
            '/core/native-harness file-reject /fixtures/ppm-duplicate.j2k /fixtures/ppt-duplicate.j2k && '
            '/core/native-harness heights /fixtures/708-singleht-one-codeblock-cleanup-sigprop-separated-by-layer.j2k && '
            '/core/native-harness fault 1 /fixtures/growth-outer.j2k && '
            '/core/native-harness fault 2 /fixtures/growth-inner.j2k')
    receipt = {'status': 'pass', 'compiler': pins['compiler'], 'sanitizers': ['address', 'undefined', 'leak'],
               'faultSites': ['outer precinct growth', 'inner code-block growth'], 'failuresPerSite': 25,
               'htBlockHeightMutations': 64,
               'fixtures': {p.name: builder.digest(p) for p in fixtures.iterdir() if p.suffix in ('.j2k', '.jph', '.jp2')}}
    (work / 'native-receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt, indent=2))


if __name__ == '__main__':
    main()
