#!/usr/bin/env python3
"""Build and verify an Aide-only native candidate; never publish or install plugins.

Run once per native Linux CPU. Publication and version changes belong to the
calling workflow. Heavy Docker work happens only through this explicit CLI.
"""
import argparse
import hashlib
import json
import os
import platform
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile
import uuid
import zipfile

ROOT = Path(__file__).resolve().parents[2]


def run(args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def capture(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def sha256(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(8 << 20), b''):
            h.update(block)
    return h.hexdigest()


def native_platform(arch, system=None, machine=None):
    system = sys.platform if system is None else system
    machine = platform.machine().lower() if machine is None else machine.lower()
    if system != 'linux' or machine not in {
        'arm64': ('aarch64', 'arm64'), 'amd64': ('x86_64', 'amd64')
    }[arch]:
        raise ValueError('Candidate builds require the matching native Linux runner')


def version_tag(version):
    if not re.fullmatch(r'\d+\.\d+\.\d+\.\d+ RC[1-9]\d*', version):
        raise ValueError('Use the existing scripts/version.sh version rules')
    return 'v' + version.replace(' RC', '-RC')


def audit_tracked(names):
    for name in names:
        path = PurePosixPath(name)
        if path.is_absolute() or '..' in path.parts:
            raise ValueError('Unsafe tracked source path: ' + name)
        if name.startswith('plugins/') and not name.startswith('plugins/testdata/'):
            raise ValueError('Installed plugin payload must not enter Aide candidate: ' + name)
        if any(x in path.parts for x in ('.agent-state', '.aide-data', 'access-token', 'issuer-private.pem')):
            raise ValueError('Private state must not be tracked: ' + name)


def audit_image_layers(archive, allow_development_tools=False):
    """Check every referenced layer, including subsequently deleted payloads."""
    with tarfile.open(archive, 'r|gz') as outer:
        for item in outer:
            if item.name == 'manifest.json':
                manifests = json.load(outer.extractfile(item))
                if len(manifests) != 1:
                    raise ValueError('Expected one saved image')
                layers = set(manifests[0]['Layers'])
                break
        else:
            raise ValueError('Missing saved-image manifest')
    seen = set()
    entries = 0
    with tarfile.open(archive, 'r|gz') as outer:
        for blob in outer:
            if blob.name not in layers or not blob.isfile():
                continue
            seen.add(blob.name)
            with tarfile.open(fileobj=outer.extractfile(blob), mode='r|*') as inner:
                for item in inner:
                    entries += 1
                    parts = PurePosixPath(item.name).parts
                    normalized = '/'.join(x for x in parts if x != '.')
                    if not allow_development_tools and (normalized == 'usr/local/go' or normalized.startswith('usr/local/go/') or
                        normalized == 'usr/lib/gcc' or normalized.startswith('usr/lib/gcc/') or
                        normalized == 'usr/local/lib/node_modules' or normalized.startswith('usr/local/lib/node_modules/') or
                        normalized in ('usr/bin/gcc', 'usr/bin/g++', 'usr/bin/make', 'usr/local/bin/npm', 'usr/local/bin/npx')):
                        raise ValueError('Development tool payload in runtime layer: ' + item.name)
                    # System libraries (e.g. Kerberos) legitimately use plugins/.
                    # Aide install roots and any id/main plugin manifest are forbidden.
                    if 'plugins' in parts:
                        index = parts.index('plugins')
                        following = parts[index + 1:]
                        aide_root = index == 0 or parts[index - 1] in ('aide', 'data', 'src', 'workspace')
                        if aide_root and following and following[0] != 'testdata':
                            raise ValueError('Plugin payload in image layer: ' + item.name)
                    if '.upgrade-backup-' in item.name or '.professional-project-state' in parts:
                        raise ValueError('Private plugin state in image layer')
                    if item.isfile() and item.name.endswith('/manifest.json'):
                        if item.size > 4 << 20:
                            raise ValueError('Oversized manifest in image layer')
                        try:
                            value = json.load(inner.extractfile(item))
                        except (ValueError, UnicodeError):
                            continue
                        if isinstance(value, dict) and 'id' in value and 'main' in value:
                            raise ValueError('Installed plugin manifest in image layer: ' + item.name)
    if not layers or seen != layers:
        raise ValueError('Not every referenced image layer was audited')
    return {'status': 'pass', 'layers': len(seen), 'entries': entries, 'pluginPayloads': 0}


FULL_PACKAGE_STARTUP_FILES = (
    'aide-launcher.exe', 'stop.bat', 'clean-docker-cache.bat',
    'start.command', 'start.sh', 'start.bat', 'compose.yaml', '.env.example',
    'scripts/aide.sh', 'scripts/launcher-data.sh',
    'scripts/launcher-data.py', 'scripts/resource-guard.sh',
    'scripts/prebuilt-image.sh',
    'scripts/local-mounts-control.sh',
    'scripts/message-lan-control.sh',
    'scripts/watch-port.sh',
    'scripts/update-agent.sh',
)


def validate_full_package(path, tag, arch, image_id):
    prefix = f'aide-{tag}-full-linux-{arch}'
    with zipfile.ZipFile(path) as bundle:
        names = bundle.namelist()
        if len(names) != len(set(names)):
            raise ValueError('Duplicate package members')
        for required in FULL_PACKAGE_STARTUP_FILES:
            if prefix + '/' + required not in names:
                raise ValueError('Missing full-package startup file: ' + required)
        for name in names:
            parts = PurePosixPath(name).parts
            if not parts or parts[0] != prefix or '..' in parts or name.startswith('/'):
                raise ValueError('Unsafe package path')
            if any(x in parts for x in ('.env', '.aide-data', 'access-token', 'issuer-private.pem', 'plugins')):
                raise ValueError('Private state or plugin in candidate package')
            if any(x in parts for x in ('workspace', 'context')) and not name.endswith('/'):
                raise ValueError('User data in candidate package')
        manifest = json.loads(bundle.read(prefix + '/docker-images/manifest.json'))
        if (manifest['releaseTag'], manifest['platform'], manifest['imageId']) != (tag, 'linux/' + arch, image_id):
            raise ValueError('Package image identity mismatch')
        sums = bundle.read(prefix + '/docker-images/SHA256SUMS').decode().split()
        if len(sums) != 2 or sums[1] != manifest['imageArchive'] or '/' in sums[1]:
            raise ValueError('Invalid image checksum manifest')
        h = hashlib.sha256()
        with bundle.open(prefix + '/docker-images/' + sums[1]) as stream:
            for block in iter(lambda: stream.read(8 << 20), b''):
                h.update(block)
        if h.hexdigest() != sums[0]:
            raise ValueError('Image archive checksum mismatch')
        if bundle.read(prefix + '/.aide-image').decode().split()[1:] != [image_id, 'linux/' + arch, tag]:
            raise ValueError('Launcher image slot identity mismatch')
        if bundle.testzip() is not None:
            raise ValueError('Candidate ZIP CRC mismatch')
    return {'status': 'pass', 'imageId': image_id, 'archiveSHA256': h.hexdigest()}


def smoke(image, arch, commit, version):
    name = 'aide-ci-smoke-' + uuid.uuid4().hex[:12]
    # Credentials are read inside this disposable container and never printed.
    script = r'''
const fs=require('fs'),https=require('https');
const token=fs.readFileSync('/data/auth/access-token','utf8').trim();
const ca=fs.readFileSync('/data/certs/cert.pem');
function get(path,auth=true){return new Promise((resolve,reject)=>{const q=https.get({hostname:'127.0.0.1',servername:'localhost',port:8080,path,ca,headers:{Host:'localhost:8080',...(auth?{Authorization:'Bearer '+token}:{})}},r=>{let b='';r.on('data',x=>b+=x);r.on('end',()=>resolve({status:r.statusCode,data:JSON.parse(b)}));});q.on('error',reject);});}
(async()=>{const h=await get('/healthz',false),u=await get('/api/sessions',false),c=await get('/api/config'),s=await get('/api/updates/slots'),p=await get('/api/plugins');
if(h.status!==200||h.data.integrity!=='ok'||u.status!==401||c.status!==200||s.status!==200||s.data.runtimeMode!=='release-image'||p.status!==200||p.data.plugins.length!==0)throw Error('Candidate health/auth/runtime/plugin isolation failed');
if(c.data.version!==VERSION||(c.data.commit||c.data.buildCommit)!==COMMIT)throw Error('Candidate build identity mismatch');
console.log(JSON.stringify({status:'pass',commit:COMMIT,version:VERSION,installedPlugins:0,runtimeMode:s.data.runtimeMode}));})().catch(e=>{console.error(e.message);process.exitCode=1});
'''.replace('VERSION', json.dumps(version)).replace('COMMIT', json.dumps(commit))
    try:
        run(['docker', 'run', '-d', '--network', 'none', '--memory', '1g', '--cpus', '1',
             '-e', 'AIDE_BUILTIN_PLUGINS=', '--name', name, image], stdout=subprocess.DEVNULL)
        run(['docker', 'exec', name, 'sh', '-c',
             'for i in $(seq 1 60); do curl --cacert /data/certs/cert.pem -fsS https://localhost:8080/healthz >/dev/null 2>&1 && exit 0; sleep 1; done; exit 1'])
        result = run(['docker', 'exec', '-i', name, 'node'], input=script, text=True, capture_output=True)
        return json.loads(result.stdout)
    finally:
        subprocess.run(['docker', 'rm', '-f', name], cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def reload_archive(image, archive, arch, image_id, source, commit, version):
    # The tag is created by this helper. Never force removal, prune images,
    # or touch a slot/production tag: an in-use image must fail this gate.
    run(['docker', 'image', 'rm', image])
    run(['docker', 'load', '--input', str(archive)])
    info = json.loads(capture(['docker', 'image', 'inspect', image]))[0]
    if (info['Id'], info['Os'], info['Architecture'],
            info['Config']['Labels'].get('aide.srcsha')) != (image_id, 'linux', arch, source):
        raise ValueError('Reloaded image identity mismatch')
    return {'status': 'pass', 'imageId': image_id, 'platform': 'linux/' + arch,
            'sourceSHA256': source, 'smoke': smoke(image, arch, commit, version)}


def verification_plan(image):
    # The standard full profile defaults runtime/optional-plugin boundary to aide:local. Use explicit
    # image arguments here instead of retagging a shared local image globally.
    return [
        (['python3', 'scripts/agent-route.py', 'verify', 'quick'], {}),
        (['python3', 'scripts/test_runtime_assets.py', image, '--require-lan-bridge'], {}),
        (['python3', 'scripts/test_go_snapshot.py'], {'AIDE_TEST_TOOLCHAIN_IMAGE': image + '-test-tools', 'AIDE_TEST_WORKERS': '2'}),
        (['python3', 'scripts/test_optional_step.py', image], {}),
    ]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--arch', choices=['arm64', 'amd64'], required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    native_platform(args.arch)
    run(['bash', 'scripts/version.sh', 'check'])
    version = capture(['bash', 'scripts/version.sh', 'show'])
    tag = version_tag(version)
    commit = capture(['git', 'rev-parse', 'HEAD'])
    if capture(['git', 'status', '--porcelain']):
        raise ValueError('Build from a clean reviewed checkout')
    audit_tracked(capture(['git', 'ls-files']).splitlines())
    source = capture(['bash', 'scripts/aide.sh', 'source-sha'])
    out = args.out.resolve()
    if out.exists() and any(out.iterdir()):
        raise ValueError('Use an empty output directory')
    out.mkdir(parents=True, exist_ok=True)
    image = f'aide:ci-{commit[:12]}-{args.arch}'
    run(['docker', 'build', '--platform', 'linux/' + args.arch, '--progress', 'plain',
         '--build-arg', 'AIDE_VERSION=' + version, '--build-arg', 'AIDE_COMMIT=' + commit,
         '--build-arg', 'AIDE_RUNTIME_MODE=release-image', '--build-arg', 'AIDE_SRC_SHA=' + source,
         '--build-arg', 'AIDE_RUN_TESTS=1', '--build-arg', 'PIP_INDEX_URL=https://pypi.org/simple', '-t', image, '.'])
    info = json.loads(capture(['docker', 'image', 'inspect', image]))[0]
    if info['Os'] != 'linux' or info['Architecture'] != args.arch or info['Config']['Labels'].get('aide.srcsha') != source:
        raise ValueError('Built image identity mismatch')
    # Race/vet use the same source in a dedicated toolchain, never the slim runtime.
    run(['python3', 'scripts/development-toolbox.py', 'build',
         '--runtime', image, '--image', image + '-test-tools'])
    verification = []
    for command, extra_env in verification_plan(image):
        run(command, env=dict(os.environ, **extra_env))
        verification.append({'command': command, 'environmentOverrides': extra_env, 'status': 'pass'})
    quick = json.loads((ROOT / '.agent-state/quick.json').read_text())
    if quick['status'] != 'pass':
        raise ValueError('Quick verification receipt did not pass')
    (out / 'quick-proof.json').write_text(json.dumps(quick, indent=2) + '\n')
    result = smoke(image, args.arch, commit, version)
    run(['bash', 'scripts/package-release-assets.sh', tag, image, str(out)])
    archive = out / f'aide-{tag}-linux-{args.arch}-image.tar.gz'
    privacy = audit_image_layers(archive)
    bundle = out / f'aide-{tag}-full-linux-{args.arch}.zip'
    package = validate_full_package(bundle, tag, args.arch, info['Id'])
    imported = reload_archive(image, archive, args.arch, info['Id'], source, commit, version)
    from toolbox_candidate import package as package_toolbox
    toolbox = package_toolbox(image + '-test-tools', tag, args.arch, source, commit, out)
    if source != capture(['bash', 'scripts/aide.sh', 'source-sha']) or commit != capture(['git', 'rev-parse', 'HEAD']):
        raise ValueError('Source changed during candidate build')
    proof = {'status': 'pass', 'candidateOnly': True, 'published': False, 'pluginsBundled': False,
             'platform': 'linux/' + args.arch, 'commit': commit, 'sourceSHA256': source,
             'version': version, 'tag': tag, 'image': image, 'imageId': info['Id'],
             'verification': verification, 'verificationProfile': 'quick plus explicit candidate-image runtime/race/vet/optional-plugin boundary; not a standard full-profile receipt',
             'smoke': result, 'privacy': privacy, 'packageValidation': package, 'importValidation': imported,
             'toolbox': toolbox,
             'asset': {'name': bundle.name, 'sha256': sha256(bundle), 'bytes': bundle.stat().st_size}}
    (out / 'candidate-proof.json').write_text(json.dumps(proof, indent=2) + '\n')
    print(json.dumps(proof))


if __name__ == '__main__':
    main()
