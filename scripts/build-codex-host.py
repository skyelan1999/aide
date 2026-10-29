#!/usr/bin/env python3
"""Build pinned public host components into an Aide image; no runtime download.
An offline source archive may be passed as --archive. SHA-256 is always checked.
"""
import argparse,hashlib,json,os,pathlib,re,shutil,subprocess,tarfile,tempfile,urllib.request

def build(pin,out,archive=None):
    lock=json.loads(pathlib.Path(pin).read_text())
    commit=lock['commit'];digest=lock['sha256'];version=lock['version']
    if not re.fullmatch(r'[a-f0-9]{40}',commit) or not re.fullmatch(r'[a-f0-9]{64}',digest) or not re.fullmatch(r'\d+\.\d+\.\d+',version):raise ValueError('Invalid source lock')
    with tempfile.TemporaryDirectory(prefix='codex-host-build-') as tmp:
        tmp=pathlib.Path(tmp);source=tmp/'source.tar.gz'
        if archive:shutil.copyfile(archive,source)
        else:
            url='https://codeload.github.com/skyelan1999/aide-pkg-CodexBridge/tar.gz/'+commit
            with urllib.request.urlopen(url,timeout=60) as response,source.open('wb') as output:
                total=0
                while True:
                    chunk=response.read(1<<20)
                    if not chunk:break
                    total+=len(chunk)
                    if total>32<<20:raise ValueError('Source archive too large')
                    output.write(chunk)
        if hashlib.sha256(source.read_bytes()).hexdigest()!=digest:raise ValueError('Source checksum mismatch')
        root=tmp/'source';root.mkdir();size=0
        with tarfile.open(source) as tar:
            for member in tar:
                relative=pathlib.PurePosixPath(member.name)
                if relative.is_absolute() or '..' in relative.parts or not (member.isfile() or member.isdir()):raise ValueError('Unsafe source archive')
                if len(relative.parts)<2:continue
                dest=root.joinpath(*relative.parts[1:]);size+=member.size
                if size>128<<20:raise ValueError('Expanded source too large')
                if member.isdir():dest.mkdir(parents=True,exist_ok=True)
                else:
                    dest.parent.mkdir(parents=True,exist_ok=True)
                    with tar.extractfile(member) as src,dest.open('wb') as dst:shutil.copyfileobj(src,dst)
        manifest=json.loads((root/'manifest.json').read_text());assert manifest['version']==version,'Version mismatch'
        subprocess.run(['node','scripts/build.cjs'],cwd=root,check=True)
        subprocess.run(['python3','scripts/package-codex-bridge.py','--out','dist'],cwd=root,check=True)
        shutil.copyfile(root/'dist'/('codex-bridge-'+version+'.zip'),root/'cmd/host-manager/payload.zip')
        out=pathlib.Path(out);out.mkdir(parents=True,exist_ok=True);sums=[]
        for platform in ('darwin','linux','windows'):
            for arch in ('amd64','arm64'):
                name='codex-bridge-'+platform+'-'+arch+('.exe' if platform=='windows' else '')
                destination=(out/name).resolve()
                flags='-s -w'+(' -H=windowsgui' if platform=='windows' else '')
                subprocess.run(['go','build','-trimpath','-ldflags',flags,'-o',str(destination),'.'],cwd=root/'cmd/host-manager',env={**os.environ,'CGO_ENABLED':'0','GOOS':platform,'GOARCH':arch,'GOWORK':'off'},check=True)
                sums.append(hashlib.sha256(destination.read_bytes()).hexdigest()+'  '+name)
        (out/'SHA256SUMS').write_text('\n'.join(sums)+'\n');(out/'VERSION').write_text(version+'\n');shutil.copyfile(root/'LICENSE',out/'LICENSE-CodexBridge.txt')
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--pin',default='docker/codex-host-source.json');p.add_argument('--out',default='/opt/aide-host-components');p.add_argument('--archive');args=p.parse_args();build(args.pin,args.out,args.archive)
