#!/usr/bin/env python3
"""Real PS prebuilt importer with verified gzip/tar data and Docker/HTTP adapters.

Tests extraction/identity/activation ordering without a real daemon or download.
Native Windows and importing the complete public image are separate checks.
"""
import argparse
import base64
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
IMAGE = 'sha256:' + 'a' * 64
CANONICAL = 'sha256:' + 'b' * 64
SOURCE = 'c' * 64
PAYLOAD = b'owned Docker load payload; never an actual application'
DRIVER = r'''
$ErrorActionPreference='Stop'
. $env:FIXTURE_SOURCE
$script:loaded=$false;$script:calls=@()
function Invoke-WebRequest {throw 'Unexpected download in cached/installed fixture'}
function Get-Command {param($Name,$ErrorAction) if($Name -eq 'tar.exe'){[pscustomobject]@{Source=$(if($env:FIXTURE_CASE -eq 'tar-denied'){'denied-tar.exe'}else{'fixture-tar.exe'})}}else{throw 'Unexpected command lookup'}}
function Invoke-Native {
 param($File,[string[]]$Arguments,[switch]$Capture)
 $script:calls+=,(@($File)+$Arguments)
 if($File -eq 'denied-tar.exe'){throw 'fixture tar.exe execution denied'}
 if($File -eq 'fixture-tar.exe'){
  $path=Join-Path $Arguments[3] $Arguments[4]
  New-Item -ItemType Directory -Path (Split-Path -Parent $path) -Force|Out-Null
  [IO.File]::WriteAllBytes($path,[Convert]::FromBase64String($env:FIXTURE_PAYLOAD))
  return [pscustomobject]@{Code=0;Text=''}
 }
 $text='';$code=0
 if($Arguments[0] -eq 'info'){$text='linux/x86_64'}
 elseif($Arguments[0] -eq 'image' -and $Arguments[1] -eq 'inspect'){
  $reference=$Arguments[2];$format=$Arguments[-1]
  if($format -like '*index .Config.Labels*'){return [pscustomobject]@{Code=64;Text='fixture PS5.1 quoted Go-template failure'}}
  if($reference -eq 'aide:local'){$text='sha256:'+('d'*64)}
  elseif($reference -eq 'EXPECTED' -and ($env:FIXTURE_CASE -eq 'installed' -or $script:loaded)){
   if($env:FIXTURE_CASE -in @('load-reference','load-id-reference','ambiguous','empty-output')){$code=1}else{$text='CANONICAL'}
  }elseif($reference -in @('aide:from-this-load','CANONICAL') -and $script:loaded){$text='CANONICAL'}
  else{$code=1}
  if($code -eq 0 -and $format -eq '{{json .}}'){
   $platform=if($env:FIXTURE_CASE -in @('wrong-platform','local-wrong-platform')){'linux/arm64'}else{'linux/amd64'}
   $src=if($env:FIXTURE_CASE -eq 'wrong-source'){'e'*64}elseif($env:FIXTURE_CASE -eq 'local-invalid-source'){'unknown'}else{'SOURCE'}
   $cad=if($env:FIXTURE_CASE -eq 'wrong-cad'){'missing'}elseif($env:FIXTURE_CASE -like 'local-*'){''}else{'8.0.1.1.0'}
   if($env:FIXTURE_CASE -eq 'local-typed-source'){$src=@('SOURCE')}
   $parts=$platform.Split('/')
   $text=@{Id=$text;Os=$parts[0];Architecture=$parts[1];Config=@{Labels=@{'aide.srcsha'=$src;'aide.cad-runtime'=$cad}}}|ConvertTo-Json -Depth 4 -Compress
   if($env:FIXTURE_CASE -eq 'local-malformed-json'){$text='{private-fixture-sentinel'}
  }
 }
 elseif($Arguments[0] -eq 'image' -and $Arguments[1] -eq 'load'){
  $actual=[Convert]::ToBase64String([IO.File]::ReadAllBytes($Arguments[-1]))
  if($actual -cne $env:FIXTURE_PAYLOAD){throw 'Wrong inner archive bytes'}
  $script:loaded=$true
  $text=if($env:FIXTURE_CASE -eq 'ambiguous'){"Loaded image: aide:from-this-load`nLoaded image: aide:other"}elseif($env:FIXTURE_CASE -eq 'empty-output'){''}elseif($env:FIXTURE_CASE -eq 'load-id-reference'){'Loaded image ID: CANONICAL'}else{'Loaded image: aide:from-this-load'}
 }
 elseif($Arguments[0] -eq 'image' -and $Arguments[1] -eq 'tag'){}
 else{throw "Unexpected fixture native call: $File $Arguments"}
 [pscustomobject]@{Code=$code;Text=$text}
}
$failed=$false;$message=''
try {if($env:FIXTURE_CASE -like 'local-*'){$failed=-not(Test-AideLocalRuntime 'fixture-docker' 'aide:local')}else{Import-AidePrebuiltImage $PSScriptRoot 'fixture-docker' 'aide:local' *> $null}}catch{$failed=$true;$message=$_.Exception.Message}
@{failed=$failed;message=$message;loaded=$script:loaded;calls=$script:calls;temporary=@(Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot 'docker-images') -Directory).Count}|ConvertTo-Json -Depth 5 -Compress
'''.replace('EXPECTED', IMAGE).replace('CANONICAL', CANONICAL).replace("'SOURCE'", "'" + SOURCE + "'")


def archive(case):
    data = io.BytesIO()
    with tarfile.open(fileobj=data, mode='w', format=tarfile.PAX_FORMAT) as tar:
        unrelated = tarfile.TarInfo('../must-not-extract')
        unrelated.size = 3
        tar.addfile(unrelated, io.BytesIO(b'no!'))
        if case == 'gnu-path':
            long_data = b'aide-offline/docker-images/aide-local.tar\0'
            long_name = tarfile.TarInfo('././@LongLink')
            long_name.type = tarfile.GNUTYPE_LONGNAME
            long_name.size = len(long_data)
            tar.addfile(long_name, io.BytesIO(long_data))
        item = tarfile.TarInfo('placeholder' if case in ('pax-path', 'gnu-path') else 'aide-offline/docker-images/aide-local.tar')
        if case == 'pax-path':
            item.pax_headers = {'path': 'aide-offline/docker-images/aide-local.tar', 'mtime': '123.456'}
        if case == 'large-pax':
            item.pax_headers = {'fixture.key': 'x' * 70000}
        if case == 'missing-member':
            item.name = 'not-the-image'
        if case == 'symlink-member':
            item.type = tarfile.SYMTYPE
            item.linkname = '../must-not-extract'
            tar.addfile(item)
        else:
            item.size = len(PAYLOAD)
            tar.addfile(item, io.BytesIO(PAYLOAD))
    raw = data.getvalue()
    if case == 'truncated-member':
        raw = raw[:1536 + 3]
    if case == 'bad-header':
        raw = b'X' + raw[1:]
    if case == 'gnu-number':
        header = bytearray(raw[1024:1536])
        header[124:136] = ((1 << 95) | len(PAYLOAD)).to_bytes(12, 'big')
        header[148:156] = b' ' * 8
        header[148:156] = f'{sum(header):06o}\0 '.encode()
        raw = raw[:1024] + header + raw[1536:]
    return gzip.compress(raw)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--pwsh', default=shutil.which('pwsh'))
    parser.add_argument('--source', type=Path, default=ROOT / 'scripts/fixtures/legacy-windows/scripts/prebuilt-image.ps1')
    parser.add_argument('--case', action='append', dest='selected')
    parser.add_argument('--docker', help='Also import a tiny owned image through a real daemon; never run it')
    args = parser.parse_args()
    if not args.pwsh:
        parser.error('PowerShell required; pass --pwsh')
    cases = args.selected or ('installed', 'canonical-load', 'load-reference', 'load-id-reference', 'pax-path', 'gnu-path', 'gnu-number', 'tar-denied',
                              'local-lean', 'local-invalid-source', 'local-wrong-platform', 'local-typed-source', 'local-malformed-json',
                              'wrong-platform', 'wrong-source', 'wrong-cad', 'ambiguous', 'empty-output',
                              'bad-checksum', 'missing-member', 'symlink-member', 'truncated-member', 'bad-header', 'large-pax')
    for case in cases:
        with tempfile.TemporaryDirectory(prefix="aide prebuilt ' space ") as folder:
            root = Path(folder) / 'work'
            root.mkdir()
            (root / 'docker').mkdir()
            (root / 'docker-images').mkdir()
            payload = archive(case)
            (root / 'docker-images/fixture.tar.gz').write_bytes(payload)
            digest = '0' * 64 if case == 'bad-checksum' else hashlib.sha256(payload).hexdigest()
            (root / 'docker/prebuilt-images.tsv').write_text(f'linux/amd64 {IMAGE} {SOURCE} {digest} https://github.com/skyelan1999/aide/releases/download/v0.1.17.1-RC1/fixture.tar.gz bundle\n')
            (root / 'driver.ps1').write_text(DRIVER, encoding='utf-8-sig')
            env = dict(os.environ, POWERSHELL_TELEMETRY_OPTOUT='1', POWERSHELL_UPDATECHECK='Off',
                       FIXTURE_SOURCE=str(args.source.resolve()), FIXTURE_CASE=case,
                       FIXTURE_PAYLOAD=base64.b64encode(PAYLOAD).decode())
            result = subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(root / 'driver.ps1')],
                                    env=env, capture_output=True, text=True, encoding='utf-8-sig', timeout=25)
            assert result.returncode == 0, (case, result.stdout, result.stderr)
            data = json.loads(result.stdout)
            assert 'private-fixture-sentinel' not in result.stdout + result.stderr
            success = case in ('installed', 'canonical-load', 'load-reference', 'load-id-reference', 'pax-path', 'gnu-path', 'gnu-number', 'tar-denied', 'local-lean')
            assert data['failed'] != success, (case, data)
            calls = data['calls']
            tags = [c for c in calls if c[1:3] == ['image', 'tag']]
            if success and not case.startswith('local-'):
                assert tags[-1] == ['fixture-docker', 'image', 'tag', CANONICAL, 'aide:local'], (case, data)
                assert data['loaded'] == (case != 'installed'), (case, data)
            else:
                assert not tags, (case, data)
            assert data['temporary'] == 0, (case, data)
            assert not (root.parent / 'must-not-extract').exists()
            if case.startswith('wrong-'):
                assert case[6:].split('-')[0].lower() in data['message'].lower(), (case, data)
            print('PASS real PowerShell prebuilt importer:', case)
    if args.docker:
        native_import(args)
    print('PASS managed fixed-member extraction and image checks; native Windows/public image import NOT_RUN')


def native_import(args):
    """Exercise actual Docker load/config-ID resolution using owned empty images."""
    platform = subprocess.check_output([args.docker, 'info', '--format', '{{.OSType}}/{{.Architecture}}'], text=True).strip().replace('/x86_64', '/amd64').replace('/aarch64', '/arm64')
    assert platform in ('linux/amd64', 'linux/arm64'), platform
    nonce = uuid.uuid4().hex
    source_tag = 'aide-issue464-import:' + nonce
    target_tag = 'aide-issue464-target:' + nonce
    config = json.dumps({'architecture': platform.split('/')[1], 'os': 'linux',
                         'config': {'Labels': {'aide.srcsha': SOURCE, 'aide.cad-runtime': '8.0.1.1.0', 'aide.owned-fixture': nonce}},
                         'rootfs': {'type': 'layers', 'diff_ids': []}}, separators=(',', ':')).encode()
    config_id = hashlib.sha256(config).hexdigest()
    oci = json.dumps({'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.manifest.v1+json',
                      'config': {'mediaType': 'application/vnd.oci.image.config.v1+json', 'digest': 'sha256:' + config_id, 'size': len(config)},
                      'layers': []}, separators=(',', ':')).encode()
    manifest_id = hashlib.sha256(oci).hexdigest()
    inner = io.BytesIO()
    with tarfile.open(fileobj=inner, mode='w') as tar:
        files = {'blobs/sha256/' + config_id: config, 'blobs/sha256/' + manifest_id: oci,
                 'oci-layout': b'{"imageLayoutVersion":"1.0.0"}',
                 'index.json': json.dumps({'schemaVersion': 2, 'manifests': [{'mediaType': 'application/vnd.oci.image.manifest.v1+json', 'digest': 'sha256:' + manifest_id, 'size': len(oci), 'annotations': {'org.opencontainers.image.ref.name': source_tag}}]}).encode(),
                 'manifest.json': json.dumps([{'Config': 'blobs/sha256/' + config_id, 'RepoTags': [source_tag], 'Layers': []}]).encode()}
        for name, data in files.items():
            item = tarfile.TarInfo(name)
            item.size = len(data)
            tar.addfile(item, io.BytesIO(data))
    with tempfile.TemporaryDirectory(prefix="aide native prebuilt ' ") as folder:
        root = Path(folder)
        (root / 'docker-images').mkdir()
        (root / 'docker').mkdir()
        archive_file = root / 'docker-images/fixture.tar.gz'
        with tarfile.open(archive_file, mode='w:gz', format=tarfile.PAX_FORMAT) as tar:
            item = tarfile.TarInfo('aide-offline/docker-images/aide-local.tar')
            item.pax_headers = {'mtime': '123.456'}
            item.size = len(inner.getvalue())
            tar.addfile(item, io.BytesIO(inner.getvalue()))
        digest = hashlib.sha256(archive_file.read_bytes()).hexdigest()
        (root / 'docker/prebuilt-images.tsv').write_text(f'{platform} sha256:{manifest_id} {SOURCE} {digest} https://github.com/skyelan1999/aide/releases/download/v0.1.17.1-RC1/fixture.tar.gz bundle\n')
        driver = root / 'driver.ps1'
        driver.write_text(r'''
$ErrorActionPreference='Stop'
$errors=$null;$tokens=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($env:FIXTURE_START,[ref]$tokens,[ref]$errors)
$native=$ast.Find({param($n)$n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Invoke-Native'},$true)
if(-not $native -or $errors.Count){throw 'Missing/invalid real native adapter'}
Invoke-Expression $native.Extent.Text
. $env:FIXTURE_SOURCE
Import-AidePrebuiltImage $PSScriptRoot $env:FIXTURE_DOCKER $env:FIXTURE_TARGET
$metadata=Invoke-Native $env:FIXTURE_DOCKER @('image','inspect',$env:FIXTURE_TARGET,'--format','{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.owned-fixture"}}') -Capture
if($metadata.Code -ne 0){throw 'Owned imported target missing'}
@{identity=$metadata.Text}|ConvertTo-Json -Compress
''', encoding='utf-8-sig')
        env = dict(os.environ, POWERSHELL_TELEMETRY_OPTOUT='1', POWERSHELL_UPDATECHECK='Off',
                   FIXTURE_START=str(ROOT / 'scripts/fixtures/legacy-windows/start.ps1'), FIXTURE_SOURCE=str(args.source.resolve()),
                   FIXTURE_DOCKER=args.docker, FIXTURE_TARGET=target_tag)
        try:
            run = subprocess.run([args.pwsh, '-NoLogo', '-NoProfile', '-File', str(driver)], env=env, text=True,
                                 capture_output=True, encoding='utf-8-sig', timeout=30)
            assert run.returncode == 0, (run.stdout, run.stderr)
            observed = json.loads(run.stdout.strip().splitlines()[-1])['identity'].split('|')
            assert observed == ['sha256:' + config_id, platform, nonce], observed
            assert manifest_id != config_id
            print('PASS real Docker import: distinct OCI manifest/config identity, verified archive and owned target tag')
        finally:
            for tag in (target_tag, source_tag):
                subprocess.run([args.docker, 'image', 'rm', tag], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
                check = subprocess.run([args.docker, 'image', 'inspect', tag], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
                assert check.returncode != 0, 'Owned fixture tag cleanup failed: ' + tag


if __name__ == '__main__':
    main()
