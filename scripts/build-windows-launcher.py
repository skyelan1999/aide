#!/usr/bin/env python3
"""Cross-compile the native C++ launcher inside a local Docker image.

An existing Go compiler builds embedded helpers; C++/SDK compilation stays in Docker.
No PowerShell invocation or system installation is used.
The resulting executable needs only Windows and Docker Desktop at runtime.
"""
import argparse
import hashlib
import json
import os
import platform
import sys
import re
import shlex
import secrets
import shutil
from pathlib import Path
import subprocess
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]
VERSION = '0.15.2'
COMPILERS = {
    'linux/amd64': ('x86_64', '02aa270f183da276e5b5920b1dac44a63f1a49e55050ebde3aecc9eb82f93239'),
    'linux/arm64': ('aarch64', '958ed7d1e00d0ea76590d27666efbf7a932281b3d7ba0c6b01b0ff26498f667f'),
}
JSON_SHA256 = 'aaf127c04cb31c406e5b04a63f1ae89369fccde6d8fa7cdda1ed4f32dfc5de63'


WEBVIEW2_VERSION = '1.0.3537.50'
WEBVIEW2_SHA256 = '5ea526bbd728adda0da4d31219267e96460494a427e4894c4e09d9f320f4b9aa'


def webview_sdk(build, arch, cache=None):
    """Build-time only, SHA256-pinned official SDK; runtime never downloads SDKs."""
    package = (cache or build) / ('webview2-' + WEBVIEW2_VERSION + '.nupkg')
    if not package.exists():
        partial = package.with_name(package.name + '.partial-' + secrets.token_hex(6))
        urllib.request.urlretrieve('https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/' +
            WEBVIEW2_VERSION + '/microsoft.web.webview2.' + WEBVIEW2_VERSION + '.nupkg', partial)
        if hashlib.sha256(partial.read_bytes()).hexdigest() != WEBVIEW2_SHA256:
            raise SystemExit('Official WebView2 SDK SHA256 mismatch')
        partial.replace(package)
    if hashlib.sha256(package.read_bytes()).hexdigest() != WEBVIEW2_SHA256:
        raise SystemExit('Cached WebView2 SDK SHA256 mismatch')
    target = build / 'webview2'
    target.mkdir(exist_ok=True)
    with zipfile.ZipFile(package) as archive:
        for member, name in [('build/native/include/WebView2.h', 'WebView2.h'),
                             ('build/native/' + ('x64' if arch == 'amd64' else 'arm64') + '/WebView2Loader.dll',
                              'WebView2Loader-' + arch + '.dll'), ('LICENSE.txt', 'LICENSE.txt')]:
            if archive.getinfo(member).file_size > 4000000:
                raise SystemExit('WebView2 SDK member exceeds build budget')
            content = archive.read(member)
            # MinGW uses lower-case include names on the Linux cross compiler.
            if name == 'WebView2.h':
                content = content.replace(b'"EventToken.h"', b'"eventtoken.h"')
            (target / name).write_bytes(content)
    (target / 'eventtoken.h').write_text('#pragma once\ntypedef struct EventRegistrationToken { long long value; } EventRegistrationToken;\n')
    return target


def helper_license_notices(module_text, source_root=ROOT):
    """Collect real pinned module notices; vendor mode has no Module.Dir."""
    roots = dict(line.split('|', 1) for line in module_text.splitlines() if '|' in line)
    notices = []
    for module, directory in sorted(roots.items()):
        base = Path(directory) if directory else source_root / 'vendor' / module
        if not base.is_dir():
            raise SystemExit('Missing embedded helper module source: ' + module)
        paths = [base / 'LICENSE'] if base.resolve() == source_root.resolve() else sorted(
            path for path in base.rglob('*') if path.is_file()
            and path.name.upper().startswith(('LICENSE', 'COPYING', 'NOTICE', 'PATENTS')))
        if (not paths or not all(path.is_file() for path in paths)
                or not any(path.name.upper().startswith(('LICENSE', 'COPYING')) for path in paths)):
            raise SystemExit('Missing embedded helper dependency license: ' + module)
        for path in paths:
            raw = path.read_bytes()
            try:
                text = raw.decode('utf-8-sig')
                encoding = 'UTF-8'
            except UnicodeDecodeError:
                # The pinned VMware BSD-2 notice is distributed as Windows-1252.
                # Preserve the original files and identify their bytes explicitly.
                text = raw.decode('cp1252')
                encoding = 'Windows-1252'
            notices.append(module + '/' + path.relative_to(base).as_posix() + '\n'
                           + 'Original SHA256: ' + hashlib.sha256(raw).hexdigest() + '\n'
                           + 'Original encoding: ' + encoding + '\n' + text)
    return notices


def host_helpers(build, arch, go, assembly_root="/build", sdk=None):
    """Build current host components into the single distributed PE, not the image's old helpers."""
    if not go:
        raise SystemExit('An existing Go 1.26+ compiler is required to embed the native host components')
    environment = dict(os.environ, GOOS='windows', GOARCH=arch, CGO_ENABLED='0', GOTOOLCHAIN='local')
    output = {}
    for role, package in [('local_mounts', './cmd/aide-local-mounts'), ('message_lan', './cmd/aide-message-lan')]:
        name = 'aide-' + role.replace('_', '-') + '-windows-' + arch + '.exe'
        destination = build / name
        subprocess.run([go, 'build', '-trimpath', '-ldflags=-s -w', '-o', str(destination), package],
                       cwd=ROOT, env=environment, check=True)
        output[role] = (name, hashlib.file_digest(destination.open('rb'), 'sha256').hexdigest())
    if sdk is not None:
        name = 'WebView2Loader-' + arch + '.dll'
        shutil.copyfile(sdk / name, build / name)
        output['webview_loader'] = (name, hashlib.file_digest((build / name).open('rb'), 'sha256').hexdigest())
    module_text = subprocess.check_output([go, 'list', '-deps', '-f',
        '{{if not .Standard}}{{with .Module}}{{.Path}}|{{.Dir}}{{end}}{{end}}',
        './cmd/aide-local-mounts', './cmd/aide-message-lan'], cwd=ROOT, env=environment, text=True)
    goroot = subprocess.check_output([go, 'env', 'GOROOT'], env=environment, text=True).strip()
    notices = ['Go standard library\n' + (Path(goroot) / 'LICENSE').read_text(encoding='utf-8')]
    notices.extend(helper_license_notices(module_text, ROOT))
    if sdk is not None:
        notices.append('Microsoft WebView2 SDK ' + WEBVIEW2_VERSION + '\n' + (sdk / 'LICENSE.txt').read_text())
    license_path = build / ('helper-licenses-' + arch + '.txt')
    license_path.write_text('\n\n'.join(notices), encoding='utf-8')
    assembly = '.section .rdata,"dr"\n'
    for role, (name, _) in output.items():
        assembly += ('.balign 16\n.globl aide_' + role + '_begin\naide_' + role + '_begin:\n'
                     '.incbin "' + assembly_root + '/' + name + '"\n.globl aide_' + role + '_end\naide_' + role + '_end:\n')
    # The compiler does not track .incbin dependencies. A content-addressed
    # source/object path prevents reuse of an older embedded PE from its cache.
    payload_key = hashlib.sha256(''.join(item[1] for item in output.values()).encode()).hexdigest()
    assembly_path = build / ('helpers-' + arch + '-' + payload_key + '.S')
    assembly_path.write_text(assembly, encoding='utf-8')
    return output, license_path, assembly_path


def source_manifest():
    """Compile the existing POSIX fingerprint's literal inventory, not a second list."""
    text = (ROOT / 'scripts/aide.sh').read_text(encoding='utf-8')
    body = text.split('source_sha() {', 1)[1].split('\n}\n', 1)[0]
    trees = shlex.split(re.search(r'^\s*find (.+?) -type d ', body, re.M)[1])
    line = next(line.strip() for line in body.splitlines() if line.strip().startswith('printf '))
    files = shlex.split(line)[2:]
    extras = shlex.split(re.search(r'for extra in (.+?); do', body)[1])
    plain = re.findall(r'then find ([A-Za-z0-9_./-]+) -type f -print0; fi', body)
    for name in trees + files + extras + plain:
        if not re.fullmatch(r'[A-Za-z0-9_./-]+', name) or name.startswith('/') or '..' in name.split('/'):
            raise ValueError('Unsupported source fingerprint inventory')
    return dict(filteredTrees=trees, plainTrees=plain, files=files + extras)


def helper_protocol_versions():
    """Use the Go shared protocol constants as the C++ launcher's source of truth."""
    source = (ROOT / 'internal/messaging/lan_bridge.go').read_text(encoding='utf-8')
    values = {}
    for go_name, macro in [('LANBridgeProtocolVersion', 'AIDE_LAN_BRIDGE_PROTOCOL_VERSION'),
                           ('LANRemoteAccessProtocolVersion', 'AIDE_LAN_REMOTE_ACCESS_PROTOCOL_VERSION')]:
        match = re.search(r'^\s*' + go_name + r'\s*=\s*([1-9][0-9]*)\s*$', source, re.M)
        if not match:
            raise SystemExit('Missing positive helper protocol constant: ' + go_name)
        values[macro] = int(match.group(1))
    return values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--image', default=os.environ.get('AIDE_LAUNCHER_BUILD_IMAGE', 'aide:local'))
    parser.add_argument('--arch', choices=('amd64', 'arm64'), default='amd64')
    parser.add_argument('--go', default=os.environ.get('AIDE_NATIVE_GO', shutil.which('go')),
                        help='Existing Go compiler used to embed the current host helpers')
    parser.add_argument('--in-container', action='store_true', help='Compile directly inside the Docker native-launcher build stage')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--desktop-test-output', type=Path, help='Build a separate synthetic UI test executable; never embedded in the product')
    parser.add_argument('--stop-test-output', type=Path, help='Separate Windows stop-recovery fault fixture; never embedded')
    args = parser.parse_args()
    if args.in_container:
        machine = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
        metadata = ['container-build-stage', 'linux/' + str(machine)]
    else:
        metadata = subprocess.check_output(['docker', 'image', 'inspect', args.image, '--format', '{{.Id}}|{{.Os}}/{{.Architecture}}'], text=True).strip().split('|')
        if len(metadata) != 2 or not re.fullmatch(r'sha256:[a-f0-9]{64}', metadata[0]) or metadata[1] not in COMPILERS:
            raise SystemExit('A local Linux amd64/arm64 build image with Python 3.12+ is required')
    identity = metadata[0]
    compiler_arch, compiler_hash = COMPILERS[metadata[1]]
    archive_name = f'zig-{compiler_arch}-linux-{VERSION}.tar.xz'
    build = ROOT / '.agent-state/native-toolchain'
    build.mkdir(parents=True, exist_ok=True)
    archive = (Path('/toolchain') / archive_name) if args.in_container else build / (f'zig-linux-{VERSION}.tar.xz' if compiler_arch == 'x86_64' else archive_name)
    archive.parent.mkdir(parents=True, exist_ok=True)
    if not archive.exists():
        partial = archive.with_suffix('.partial')
        urllib.request.urlretrieve(f'https://ziglang.org/download/{VERSION}/{archive_name}', partial)
        if hashlib.file_digest(partial.open('rb'), 'sha256').hexdigest() != compiler_hash:
            raise SystemExit('Official Linux compiler archive SHA256 mismatch')
        partial.replace(archive)
    if hashlib.file_digest(archive.open('rb'), 'sha256').hexdigest() != compiler_hash:
        raise SystemExit('Cached Linux compiler SHA256 mismatch')
    header = ROOT / 'native/windows-launcher/third_party/json.hpp'
    if hashlib.file_digest(header.open('rb'), 'sha256').hexdigest() != JSON_SHA256:
        raise SystemExit('Pinned nlohmann/json 3.12.0 header SHA256 mismatch')
    for name, expected in {
        'miniz_tinfl.c': 'ff9cc5426df23a78070b5b8fa2dcb7aea68faac466169a883b4ffb8727532350',
        'miniz_tinfl.h': 'da4850920fdf09f8877d9affabe1ebba1852ce4b1ebbd121f2ba7a7ea8e57e5d',
        'miniz_common.h': '6320fa10a5d21db9f78594d92745bc27593c1e6bed657104c22da0a9bd687b49',
        'LICENSE': '0115478d567121238cf6cc1c0c361926cf07a49d9e4c9e66da97fac6a01646b3',
    }.items():
        path = ROOT / 'native/windows-launcher/third_party/miniz' / name
        if hashlib.file_digest(path.open('rb'), 'sha256').hexdigest() != expected:
            raise SystemExit('Pinned miniz 3.1.0 source SHA256 mismatch: ' + name)
    sdk = webview_sdk(build, args.arch, Path('/toolchain') if args.in_container else None)
    embedded = '#pragma once\n'
    for name, value in helper_protocol_versions().items():
        embedded += f'#define {name} {value}\n'
    helpers, helper_licenses, helpers_source = host_helpers(build, args.arch, args.go, str(build) if args.in_container else "/build", sdk=sdk)
    for name, path in [('compose', ROOT / 'docker/compose.offline.yaml'),
                       ('data', ROOT / 'scripts/launcher-data.py'),
                       ('host_data', ROOT / 'native/windows-launcher/package_image.py'),
                       ('prebuilt_manifest', ROOT / 'docker/prebuilt-images.tsv'),
                       ('helper_licenses', helper_licenses),
                       ('json_license', ROOT / 'native/windows-launcher/third_party/LICENSE.json'),
                       ('miniz_license', ROOT / 'native/windows-launcher/third_party/miniz/LICENSE')]:
        text = path.read_text(encoding='utf-8-sig')
        if ')AIDE_EMBED"' in text:
            raise SystemExit('Embedded source delimiter collision')
        embedded += f'inline const char* embedded_{name}=R"AIDE_EMBED({text})AIDE_EMBED";\n'
    embedded += 'inline const char* embedded_host_arch="' + args.arch + '";\n'
    for role, (_, digest) in helpers.items():
        embedded += f'extern "C" const unsigned char aide_{role}_begin[], aide_{role}_end[];\n'
        embedded += f'inline const char* embedded_{role}_sha256="{digest}";\n'
    embedded += 'inline const char* embedded_source_manifest=R"AIDE_EMBED(' + json.dumps(source_manifest()) + ')AIDE_EMBED";\n'
    (build / 'embedded.hpp').write_text(embedded, encoding='utf-8')
    compiler = f'/toolchain/zig-{compiler_arch}-linux-{VERSION}/zig'
    container_code = r'''
import os, pathlib, subprocess, tarfile
compiler=pathlib.Path(COMPILER)
ready=pathlib.Path(READY_FILE)
if not ready.exists():
    with tarfile.open(ARCHIVE_PATH) as archive:
        archive.extractall('/toolchain', filter='data')
    ready.write_text('verified compiler extracted')
os.environ['ZIG_GLOBAL_CACHE_DIR']='/toolchain/zig-cache'
object_file='/build/tinfl.'+TARGET+'.o'
helpers_object=HELPERS_OBJECT
resource_file='/build/aide.'+TARGET+'.res'
subprocess.run([str(compiler),'rc','/i','/src/native/windows-launcher/resources','--','/src/native/windows-launcher/resources/aide.rc',resource_file],check=True)
subprocess.run([str(compiler),'cc','-target',TARGET,'-c',HELPERS_SOURCE,'-o',helpers_object],check=True)
subprocess.run([str(compiler),'cc','-O2','-target',TARGET,'-c',
    '/src/native/windows-launcher/third_party/miniz/miniz_tinfl.c','-o',object_file],check=True)
subprocess.run([str(compiler),'c++','-std=c++17','-O2','-target',TARGET,
    '-municode','-static','-I/build','-I/build/webview2','-ladvapi32','-lbcrypt','-lws2_32','-lshell32','-lole32','-luuid','-lwinhttp','-lcomctl32','-lcrypt32','-lgdi32','-lgdiplus','-ldwmapi',
    '/src/native/windows-launcher/main.cpp',object_file,helpers_object,resource_file,'-o',OUTPUT],check=True)
'''.replace('COMPILER', repr(compiler)).replace('ARCHIVE_PATH', repr('/build/' + archive.name)).replace('READY_FILE', repr('/toolchain/extracted-' + compiler_arch + '-' + VERSION)).replace('TARGET', repr('x86_64-windows-gnu' if args.arch == 'amd64' else 'aarch64-windows-gnu')).replace('HELPERS_SOURCE', repr('/build/' + helpers_source.name)).replace('HELPERS_OBJECT', repr('/build/' + helpers_source.stem + '.o'))
    if (args.desktop_test_output or args.stop_test_output) and not args.in_container:
        raise SystemExit('Desktop test build requires the existing in-container build mode')
    destination = args.output or ROOT / ('aide-launcher.exe' if args.arch == 'amd64' else 'bin/aide-launcher-arm64.exe')
    destination.parent.mkdir(parents=True, exist_ok=True)
    output = str(destination) if args.in_container else ('/src/aide-launcher.exe' if args.arch == 'amd64' else '/src/bin/aide-launcher-arm64.exe')
    for fixture, source in [(args.desktop_test_output, 'windows_desktop_smoke.cpp'), (args.stop_test_output, 'windows_stop_recovery.cpp')]:
        if fixture:
            fixture.parent.mkdir(parents=True, exist_ok=True)
            container_code += "\nsubprocess.run([str(compiler),'c++','-std=c++17','-O2','-target'," + repr('x86_64-windows-gnu' if args.arch == 'amd64' else 'aarch64-windows-gnu') + ",'-municode','-static','-I/build','-I/build/webview2','-ladvapi32','-lbcrypt','-lws2_32','-lshell32','-lole32','-luuid','-lwinhttp','-lcomctl32','-lcrypt32','-lgdi32','-lgdiplus','-ldwmapi'," + repr('/src/scripts/' + source) + ",object_file,helpers_object,resource_file,'-o'," + repr(str(fixture)) + "],check=True)\n"
    container_code = container_code.replace('OUTPUT', repr(output))
    if args.in_container:
        container_code = container_code.replace('/build/' + archive.name, str(archive)).replace('/build/', str(build) + '/').replace("'-I/build'", repr('-I' + str(build))).replace("'-I/build/webview2'", repr('-I' + str(sdk)))
        subprocess.run([sys.executable, '-c', container_code], check=True)
    else:
        subprocess.run(['docker', 'run', '--rm', '--pull', 'never', '--network', 'none',
                        '--memory', '4g', '--cpus', '4', '--user', '0:0', '--no-healthcheck',
                        '-v', f'{ROOT}:/src', '-v', f'{build}:/build',
                        '--mount', 'type=volume,source=aide-native-launcher-toolchain-v0152,target=/toolchain',
                        '--entrypoint', 'python3', identity, '-c', container_code], check=True)
    binary = destination.read_bytes()
    for role, (name, digest) in helpers.items():
        payload = (build / name).read_bytes()
        if payload not in binary or digest.encode() not in binary:
            raise SystemExit('Embedded helper payload/SHA256 does not match current build: ' + role)
    if b'Go standard library' not in binary:
        raise SystemExit('Embedded Go dependency notices are missing')
    symbols = destination.with_suffix('.pdb')
    if symbols.exists():
        symbols.replace(build / f'aide-launcher-{args.arch}.pdb')
    print(f'Built {destination} ({destination.stat().st_size} bytes)')
    print('SHA256 ' + hashlib.file_digest(destination.open('rb'), 'sha256').hexdigest())


if __name__ == '__main__':
    main()
