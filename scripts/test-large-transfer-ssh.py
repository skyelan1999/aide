#!/usr/bin/env python3
"""Owned loopback OpenSSH fixture for large transfers. Never uses product sockets."""
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time

if os.name != 'posix' or not Path('/usr/sbin/sshd').is_file():
    raise SystemExit('Requires OpenSSH server and client on a POSIX host')
username = __import__('pwd').getpwuid(os.getuid()).pw_name
if username != 'agent':
    raise SystemExit('Fixture currently requires the unprivileged agent account')
with tempfile.TemporaryDirectory(prefix='aide-transfer-ssh-') as directory:
    root = Path(directory)
    for name in ('host', 'client'):
        subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(root/name)], check=True)
    shutil.copyfile(root/'client.pub', root/'authorized_keys')
    (root/'remote').mkdir()
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    (root/'config').write_text(f'''ListenAddress 127.0.0.1
Port {port}
HostKey {root}/host
PidFile {root}/pid
AuthorizedKeysFile {root}/authorized_keys
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
UsePAM no
AllowUsers {username}
Subsystem sftp /usr/lib/openssh/sftp-server
LogLevel ERROR
''')
    wrapper = root/'ssh-wrapper'
    wrapper.write_text('''#!/usr/bin/env python3
import os,sys
args=sys.argv[1:]
for i,arg in enumerate(args):
    if arg == '-S': args[i+1] = os.environ['AIDE_TRANSFER_SSH_SOCKET']
    elif arg.startswith('ControlPath='): args[i] = 'ControlPath='+os.environ['AIDE_TRANSFER_SSH_SOCKET']
os.execv('/usr/bin/ssh',['ssh']+args)
''')
    wrapper.chmod(0o700)
    sock = str(root/'master.sock')
    target = username+'@127.0.0.1'
    with (root/'sshd.log').open('wb') as log:
        server = subprocess.Popen(['/usr/sbin/sshd', '-D', '-e', '-f', str(root/'config')], stdout=log, stderr=log)
        master = None
        try:
            for attempt in range(30):
                if server.poll() is not None:
                    raise RuntimeError((root/'sshd.log').read_text())
                try:
                    with socket.create_connection(('127.0.0.1', port), timeout=.1):
                        break
                except OSError:
                    time.sleep(.1)
            master = subprocess.Popen(['ssh', '-MN', '-S', sock, '-p', str(port), '-i', str(root/'client'), '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=accept-new', '-o', f'UserKnownHostsFile={root}/known_hosts', target], stdout=subprocess.DEVNULL, stderr=log)
            for attempt in range(50):
                if Path(sock).exists(): break
                if master.poll() is not None: raise RuntimeError((root/'sshd.log').read_text())
                time.sleep(.1)
            env = dict(os.environ, AIDE_TRANSFER_SSH_ROOT=str(root), AIDE_TRANSFER_SSH_SOCKET=sock, AIDE_TRANSFER_SSH_PORT=str(port))
            result = subprocess.run(['go','test','./internal/server','-run','^TestLargeTransferSSHLive$','-count=1','-v','-timeout=4m'], env=env)
            if result.returncode: raise SystemExit(result.returncode)
        finally:
            if master is not None:
                master.terminate()
                try: master.wait(timeout=5)
                except subprocess.TimeoutExpired: master.kill(); master.wait()
            server.terminate()
            server.wait(timeout=5)
