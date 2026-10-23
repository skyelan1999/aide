"""Build-independent SDK bundle validation and actual compiler/import checks."""
import json
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
PROGRAM = r'''
import json, os, subprocess, tempfile
from pathlib import Path
assert os.geteuid() != 0
assert os.environ['AIDE_RUNTIME_PROFILE'] == 'toolbox'
def output(args):
    return subprocess.check_output(args, text=True).strip()
assert output(['go', 'version']).split()[2].startswith('go1.26.')
assert output(['node', '--version']).startswith('v24.')
assert output(['npm', '--version'])
with tempfile.TemporaryDirectory() as d:
    root=Path(d)
    (root/'main.go').write_text('package main\nimport "fmt"\nfunc main(){fmt.Println("go-sdk-ok")}')
    (root/'main.cpp').write_text('#include <iostream>\nint main(){std::cout << "cpp-sdk-ok";}')
    assert output(['go', 'run', str(root/'main.go')]) == 'go-sdk-ok'
    subprocess.run(['g++', str(root/'main.cpp'), '-o', str(root/'app')], check=True)
    assert output([str(root/'app')]) == 'cpp-sdk-ok'
print(json.dumps({'status':'pass','go':output(['go','version']),'npm':output(['npm','--version']), 'cppCompiled':True,'uid':os.geteuid(),'network':'none'}))
'''


def exercise(image):
    value = subprocess.check_output(['docker', 'run', '--rm', '--network', 'none',
        '--memory', '1g', '--cpus', '1', '--workdir', '/tmp', '--entrypoint', 'python3',
        image, '-c', PROGRAM], text=True, timeout=180)
    return json.loads(value)


def validate_bundle(path, tag, arch, source):
    prefix = f'aide-{tag}-devtools-linux-{arch}'
    required = {'image.tar.gz', 'manifest.json', 'SHA256SUMS', 'README.md',
                'scripts/development-toolbox.py', 'docker/Dockerfile.sdk', 'LICENSE'}
    with zipfile.ZipFile(path) as bundle:
        names = bundle.namelist()
        if len(names) != len(set(names)) or set(names) != {prefix + '/' + name for name in required}:
            raise ValueError('Unexpected SDK bundle inventory')
        for item in bundle.infolist():
            if '..' in PurePosixPath(item.filename).parts or stat.S_ISLNK(item.external_attr >> 16):
                raise ValueError('Unsafe SDK bundle member')
        manifest = json.loads(bundle.read(prefix + '/manifest.json'))
        if (manifest.get('format'), manifest.get('releaseTag'), manifest.get('platform'),
            manifest.get('sourceSHA256'), manifest.get('imageProfile'), manifest.get('imageArchive')) != (
            'aide-development-toolbox', tag, 'linux/' + arch, source, 'toolbox', 'image.tar.gz'):
            raise ValueError('SDK bundle identity mismatch')
        sums = bundle.read(prefix + '/SHA256SUMS').decode().split()
        import hashlib
        digest = hashlib.sha256()
        with bundle.open(prefix + '/image.tar.gz') as stream:
            while block := stream.read(8 << 20): digest.update(block)
        if sums != [digest.hexdigest(), 'image.tar.gz'] or bundle.testzip() is not None:
            raise ValueError('SDK bundle checksum mismatch')
    return {'status': 'pass', 'archiveSHA256': digest.hexdigest()}


def package(image, tag, arch, source, commit, out):
    from build_candidate import capture, run, sha256, audit_image_layers
    info = json.loads(capture(['docker', 'image', 'inspect', image]))[0]
    if (info['Os'], info['Architecture'], info['Config']['Labels'].get('aide.runtime-profile'),
        info['Config']['Labels'].get('aide.srcsha')) != ('linux', arch, 'toolbox', source):
        raise ValueError('SDK candidate identity mismatch')
    checks = exercise(image)
    name = f'aide-{tag}-devtools-linux-{arch}.zip'
    target = out / name
    if target.exists(): raise ValueError('Refuse SDK bundle overwrite')
    with tempfile.TemporaryDirectory(prefix='aide-sdk-') as temporary:
        root = Path(temporary) / name.removesuffix('.zip')
        (root / 'scripts').mkdir(parents=True)
        (root / 'docker').mkdir()
        archive = root / 'image.tar.gz'
        run(['python3', 'scripts/development-toolbox.py', 'export', '--image', image,
             '--output', str(archive)], stdout=subprocess.DEVNULL)
        (root / 'SHA256SUMS').write_text(sha256(archive) + '  image.tar.gz\n')
        Path(str(archive) + '.sha256').unlink()
        manifest = dict(format='aide-development-toolbox', releaseTag=tag, platform='linux/' + arch,
                        sourceSHA256=source, commit=commit, imageProfile='toolbox',
                        imageArchive='image.tar.gz', imageId=info['Id'])
        (root / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        (root / 'README.md').write_text(f'''# Aide {tag} optional development tools

This optional Linux {arch} container toolbox provides Go 1.26, C/C++, make,
pkg-config and npm. It is not an Aide service. Ordinary users do not need it.

Verify image.tar.gz against SHA256SUMS, then run:

    docker load --input image.tar.gz

Use the image ID printed by docker load (Docker Desktop can expose a different
local ID), then optionally name it:

    docker tag LOADED_IMAGE_ID aide:toolbox
    python3 scripts/development-toolbox.py run --image aide:toolbox --workspace /absolute/project -- go version

The helper needs Python 3 on the host; plain docker commands also work. It runs
UID 1000, only mounts the selected project, and defaults to network none.
Add --network before -- only when dependency downloads are needed. Project
outputs persist; temporary HOME/cache does not. There is no automatic assistant
command forwarding and no Aide data or Docker socket mount. No compilation is
needed to install this archive. Match the Docker engine architecture and this
release version. Downloading both offline packages duplicates their shared layers.
''')
        for relative in ('scripts/development-toolbox.py', 'docker/Dockerfile.sdk', 'LICENSE'):
            shutil.copyfile(ROOT / relative, root / relative)
        privacy = audit_image_layers(archive, allow_development_tools=True)
        # Test a real archive import, without overwriting any shared runtime tag.
        run(['docker', 'image', 'rm', image])
        run(['docker', 'load', '--input', str(archive)])
        imported = json.loads(capture(['docker', 'image', 'inspect', info['Id']]))[0]
        if (imported['Id'], imported['Os'], imported['Architecture'], imported['RootFS']) != (
                info['Id'], 'linux', arch, info['RootFS']):
            raise ValueError('Imported SDK identity mismatch')
        imported_checks = exercise(info['Id'])
        run(['docker', 'image', 'tag', info['Id'], image])
        with zipfile.ZipFile(target, 'x') as bundle:
            for relative in sorted(root.rglob('*')):
                if relative.is_file():
                    bundle.write(relative, str(relative.relative_to(root.parent)),
                        compress_type=zipfile.ZIP_STORED if relative == archive else zipfile.ZIP_DEFLATED)
        validated = validate_bundle(target, tag, arch, source)
    return dict(status='pass', image=image, imageId=info['Id'], platform='linux/' + arch,
                sourceSHA256=source, commit=commit, checks=checks, privacy=privacy,
                importValidation=imported_checks, packageValidation=validated,
                asset=dict(name=name, bytes=target.stat().st_size, sha256=sha256(target)))
