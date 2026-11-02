#!/usr/bin/env python3
"""Build the native Mac desktop host, or open an already compiled installation."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import tempfile

SOURCE = Path(__file__).resolve().parents[1]


def identity(root):
    return hashlib.sha256(os.fsencode(str(Path(root).resolve()))).hexdigest()[:20]


def bundle_id(root):
    return 'local.aide.desktop.' + identity(root)


def build_directory(root):
    # File Provider can attach FinderInfo to .app packages in iCloud folders.
    return Path.home() / 'Library/Caches' / bundle_id(root) / 'builds'


def owned_bundle(path, root):
    try:
        info = plistlib.loads((Path(path) / 'Contents/Info.plist').read_bytes())
        return (info.get('CFBundleIdentifier') == bundle_id(root)
                and isinstance(info.get('AideProjectRoot'), str)
                and Path(info['AideProjectRoot']).is_absolute()
                and Path(info.get('AideProjectRoot', '')).resolve() == Path(root).resolve()
                and (Path(path) / 'Contents/MacOS/AideDesktop').is_file())
    except (OSError, ValueError, plistlib.InvalidFileException):
        return False


def current_app(root):
    directory = Path(root).resolve() / '.aide-data/desktop'
    receipt = directory / 'current.json'
    if directory.is_symlink() or receipt.is_symlink():
        raise ValueError('Desktop state must not contain symbolic links')
    try:
        value = json.loads(receipt.read_text())
        digest = value['digest']
        if not re.fullmatch('[a-f0-9]{64}', digest):
            raise ValueError('Invalid desktop build identity')
        app = build_directory(root) / digest / 'Aide.app'
        installed = Path.home() / 'Applications/Aide.app'
        if not installed.is_symlink() and owned_bundle(installed, root):
            info = plistlib.loads((installed / 'Contents/Info.plist').read_bytes())
            if info.get('AideDesktopBuildDigest') == digest:
                return installed
        return app if owned_bundle(app, root) else None
    except FileNotFoundError:
        return None


def atomic_json(path, value):
    if path.is_symlink():
        raise ValueError('Desktop receipt must not be a symbolic link')
    with tempfile.NamedTemporaryFile('w', dir=path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        json.dump(value, stream, indent=2)
        stream.write('\n')
    try:
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def compile_app(root):
    root = Path(root).resolve(strict=True)
    if sys.platform != 'darwin':
        raise ValueError('The Mac desktop app requires macOS')
    state = root / '.aide-data/desktop'
    builds = build_directory(root)
    for path in (root / '.aide-data', state, builds.parent, builds):
        if path.is_symlink():
            raise ValueError('Desktop build state must not contain symbolic links')
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    builds.mkdir(parents=True, exist_ok=True, mode=0o700)
    version_text = (SOURCE / 'version.md').read_text()
    match = re.search(r'\b(\d+\.\d+\.\d+\.\d+) RC([1-9]\d*)\b', version_text)
    if not match:
        raise ValueError('Aide version identity unavailable')
    version = match[1] + ' RC' + match[2]
    sources = [SOURCE / 'native/macos-desktop/main.swift', SOURCE / 'scripts/desktop-session.py',
               SOURCE / 'scripts/render-computer-bridge-icon.swift', Path(__file__)]
    digest = hashlib.sha256()
    for source in sources:
        digest.update(source.read_bytes())
    digest.update((str(root) + version + os.uname().machine).encode())
    key = digest.hexdigest()
    app = builds / key / 'Aide.app'
    if not owned_bundle(app, root):
        print('AIDE: 编译 Mac 独立桌面窗口…', file=sys.stderr, flush=True)
        cache = root / '.cache/swift-modules'
        cache.mkdir(parents=True, exist_ok=True)
        common = ['xcrun', 'swiftc', '-swift-version', '5', '-module-cache-path', str(cache)]
        with tempfile.TemporaryDirectory(prefix='desktop-build-', dir=builds.parent) as temporary:
            staging = Path(temporary) / 'Aide.app'
            macos = staging / 'Contents/MacOS'
            resources = staging / 'Contents/Resources'
            macos.mkdir(parents=True)
            resources.mkdir()
            executable = macos / 'AideDesktop'
            subprocess.run(common + ['-O', '-target', os.uname().machine + '-apple-macos14.0',
                                      str(sources[0]), '-o', str(executable)], check=True)
            shutil.copyfile(SOURCE / 'scripts/desktop-session.py', resources / 'desktop-session.py')
            info = {'CFBundleIdentifier': bundle_id(root), 'CFBundleName': 'Aide',
                    'CFBundleDisplayName': 'Aide', 'CFBundleExecutable': 'AideDesktop',
                    'CFBundleIconFile': 'AppIcon', 'CFBundlePackageType': 'APPL',
                    'CFBundleVersion': match[1], 'CFBundleShortVersionString': match[1],
                    'LSMinimumSystemVersion': '14.0', 'NSHighResolutionCapable': True,
                    'LSApplicationCategoryType': 'public.app-category.developer-tools',
                    'NSMicrophoneUsageDescription': 'Aide 使用麦克风进行你主动开启的语音输入和通话。',
                    'NSSpeechRecognitionUsageDescription': 'Aide 仅在你主动开启语音输入时请求语音识别。',
                    'NSCameraUsageDescription': 'Aide 仅在你主动使用需要摄像头的功能时请求访问。',
                    'NSLocalNetworkUsageDescription': 'Aide 连接这台电脑上的本地工作台服务。',
                    'AideProjectRoot': str(root), 'AideDisplayVersion': version, 'AideDesktopBuildDigest': key}
            (staging / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
            renderer = Path(temporary) / 'render-icon'
            iconset = Path(temporary) / 'AppIcon.iconset'
            iconset.mkdir()
            subprocess.run(common + [str(sources[2]), '-o', str(renderer)], check=True)
            subprocess.run([str(renderer), str(iconset)], check=True)
            subprocess.run(['iconutil', '-c', 'icns', str(iconset), '-o', str(resources / 'AppIcon.icns')], check=True)
            entitlements = Path(temporary) / 'entitlements.plist'
            entitlements.write_bytes(plistlib.dumps({'com.apple.security.device.audio-input': True,
                                                     'com.apple.security.device.camera': True}))
            for attribute in ('com.apple.FinderInfo', 'com.apple.ResourceFork'):
                subprocess.run(['xattr', '-dr', attribute, str(staging)], capture_output=True)
            subprocess.run(['codesign', '--force', '--sign', '-', '--identifier', bundle_id(root),
                            '--requirements', '=designated => identifier "' + bundle_id(root) + '"',
                            '--entitlements', str(entitlements), str(staging)], check=True)
            subprocess.run(['codesign', '--verify', '--deep', '--strict', str(staging)], check=True)
            app.parent.mkdir(exist_ok=True)
            if app.exists():
                raise ValueError('Existing desktop artifact is invalid; it was preserved')
            staging.rename(app)
    return app


def install_app(app, root, destination=None):
    destination = Path(destination) if destination else Path.home() / 'Applications/Aide.app'
    destination = destination.expanduser().absolute()
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.is_symlink():
        raise ValueError('Existing Applications entry is a symlink; it was preserved')
    if destination.exists() and not owned_bundle(destination, root):
        raise ValueError('An Aide.app belonging to another installation already exists; it was preserved')
    if destination.exists():
        source_info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
        previous_info = plistlib.loads((destination / 'Contents/Info.plist').read_bytes())
        if source_info.get('AideDesktopBuildDigest') and source_info['AideDesktopBuildDigest'] == previous_info.get('AideDesktopBuildDigest'):
            return verified_app(destination)
    with tempfile.TemporaryDirectory(prefix='.aide-install-', dir=destination.parent) as temporary:
        staging = Path(temporary) / 'Aide.app'
        shutil.copytree(app, staging)
        subprocess.run(['codesign', '--verify', '--deep', '--strict', str(staging)], check=True)
        # Request normal application quit, honoring its unsaved-plugin confirmation.
        # Never force-kill a running UI or replace its on-disk executable first.
        subprocess.run([str(app / 'Contents/MacOS/AideDesktop'), '--prepare-update'], check=True, timeout=25)
        previous = Path(temporary) / 'previous.app'
        if destination.exists():
            destination.rename(previous)
        try:
            staging.rename(destination)
        except BaseException:
            if previous.exists():
                previous.rename(destination)
            raise
    subprocess.run(['/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister',
                    '-f', str(destination)], capture_output=True)
    return destination



def verified_app(app):
    subprocess.run(['codesign', '--verify', '--deep', '--strict', str(app)], check=True, capture_output=True)
    return app


def installed_current(app, root):
    destination = Path.home() / 'Applications/Aide.app'
    if not destination.is_symlink() and owned_bundle(destination, root):
        current = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
        installed = plistlib.loads((destination / 'Contents/Info.plist').read_bytes())
        if current.get('AideDesktopBuildDigest') and current['AideDesktopBuildDigest'] == installed.get('AideDesktopBuildDigest'):
            return destination
    return app


def recycle_builds(root, current, running=None):
    """Only old owned native bundles; preserve running executables and unknown state."""
    if running is None:
        processes = subprocess.run(['ps', '-axo', 'comm='], text=True, capture_output=True, check=True)
        running = processes.stdout.splitlines()
    running = {str(Path(path.strip()).resolve()) for path in running if path.strip()}
    removed = []
    for builds in (build_directory(root), Path(root).resolve() / '.aide-data/desktop/builds'):
        if builds.is_symlink():
            raise ValueError('Desktop build directory must not be a symbolic link')
        if not builds.exists():
            continue
        for directory in builds.iterdir():
            candidate = directory / 'Aide.app'
            executable = candidate / 'Contents/MacOS/AideDesktop'
            if (not directory.is_symlink() and directory.is_dir()
                    and re.fullmatch('[a-f0-9]{64}', directory.name)
                    and candidate != current and not candidate.is_symlink()
                    and set(directory.iterdir()) == {candidate} and owned_bundle(candidate, root)
                    and str(executable.resolve()) not in running):
                shutil.rmtree(directory)
                removed.append(directory.name)
    return removed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', default=str(SOURCE))
    parser.add_argument('--install', action='store_true')
    parser.add_argument('--destination')
    parser.add_argument('--open', action='store_true')
    parser.add_argument('--open-existing', action='store_true',
                        help='Open a previously built app only; never compile during start')
    args = parser.parse_args()
    root = Path(args.root).resolve(strict=True)
    try:
        if args.open_existing:
            app = current_app(root)
            if app is None:
                return 3
        else:
            app = compile_app(root)
        compiled = app
        verified_app(app)
        if args.install:
            app = install_app(app, root, args.destination)
        elif args.open_existing:
            app = verified_app(installed_current(app, root))
        if not args.open_existing:
            info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
            atomic_json(root / '.aide-data/desktop/current.json', {'version': 1, 'digest': info['AideDesktopBuildDigest'], 'bundleId': bundle_id(root)})
        if args.install:
            try:
                recycle_builds(root, compiled)
            except (OSError, ValueError, subprocess.SubprocessError):
                print('AIDE: 旧桌面构建产物暂未回收；新应用已保留。', file=sys.stderr)
        if args.open or args.open_existing:
            subprocess.run(['open', '-a', str(app), '--args', '--root', str(root)], check=True)
        print(app)
        return 0
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print('Aide desktop: ' + str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
