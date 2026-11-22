#!/usr/bin/env python3
"""Start the opt-in local phone gateway under the desktop's existing access."""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time


def control(root, action):
    root = Path(root).resolve()
    state = root/'.aide-data/sip-phone'
    config_path, record_path = state/'config.json', state/'process.json'
    if not config_path.is_file():
        return {'running': False, 'reason': 'not configured'}
    config = json.loads(config_path.read_text())
    script = root/'scripts/aide_sip_phone.py'
    python = state/'venv/bin/python'
    record = json.loads(record_path.read_text()) if record_path.is_file() else {}
    pid = record.get('pid', 0)
    command = subprocess.run(['ps', '-p', str(pid), '-o', 'command='], capture_output=True, text=True).stdout if isinstance(pid, int) and pid > 1 else ''
    owned = str(script) in command and ' --serve' in command
    identity = hashlib.sha256(script.read_bytes()+config_path.read_bytes()).hexdigest()
    if owned and action == 'start' and config.get('enabled') and record.get('identity') == identity:
        return {'running': True, 'pid': pid}
    if owned:
        os.kill(pid, signal.SIGTERM)
        for _ in range(30):
            try: os.kill(pid, 0)
            except ProcessLookupError: break
            time.sleep(.1)
    if action == 'stop' or not config.get('enabled'):
        return {'running': False}
    phone = str(ipaddress.IPv4Address(config['phone']))
    account = config.get('phone_user', '1001')
    assistant = config.get('assistant', 'auto')
    port = int(config.get('port', 5060))
    if not ipaddress.IPv4Address(phone).is_private or not re.fullmatch(r'[A-Za-z0-9_-]{1,32}', account) or not re.fullmatch(r'[a-zA-Z0-9_-]{1,64}', assistant) or not 1024 <= port <= 65535:
        raise ValueError('Invalid local SIP phone configuration')
    if not python.is_file():
        raise ValueError('Local SIP gateway dependencies are not installed')
    args = [str(python), '-u', str(script), '--phone', phone, '--phone-user', account,
            '--assistant', assistant, '--normal', '--serve', '--sip-port', str(port),
            '--call-seconds', '1800', '--data', str(root/'.aide-data/storage/data'),
            '--report', str(state/'last-call.json')]
    with (state/'gateway.log').open('a') as log:
        process = subprocess.Popen(args, cwd=root, stdin=subprocess.DEVNULL, stdout=log,
                                   stderr=subprocess.STDOUT, start_new_session=True)
    record_path.write_text(json.dumps({'pid': process.pid, 'identity': identity})+'\n')
    os.chmod(record_path, 0o600)
    return {'running': True, 'pid': process.pid}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['start', 'stop'])
    parser.add_argument('--root', required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(control(args.root, args.action)))
    except (OSError, ValueError, KeyError) as error:
        print(json.dumps({'running': False, 'error': type(error).__name__}))
        sys.exit(1)
