#!/usr/bin/env python3
"""Configure and run the opt-in local SIP handset gateway."""
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
import tempfile
import time


def parameters(config):
    phone = ipaddress.IPv4Address(config['phone'])
    account = config.get('phone_user', '1001')
    assistant = config.get('assistant', 'auto')
    port = int(config.get('port', 5060))
    phone_port = int(config.get('phone_port', 5060))
    aide_port = int(config.get('aide_port', 9999))
    if (not phone.is_private or not re.fullmatch(r'[A-Za-z0-9_-]{1,32}', account)
            or not re.fullmatch(r'[a-zA-Z0-9_-]{1,64}', assistant)
            or not 1024 <= port <= 65535 or not 1 <= phone_port <= 65535
            or not 1 <= aide_port <= 65535):
        raise ValueError('Invalid local SIP phone configuration')
    return ['--phone', str(phone), '--phone-user', account, '--assistant', assistant,
            '--sip-port', str(port), '--phone-port', str(phone_port), '--aide-port', str(aide_port)]


def configure(root, changes):
    state = Path(root).resolve()/'.aide-data/sip-phone'
    path = state/'config.json'
    config = json.loads(path.read_text()) if path.exists() else {}
    config.update({key:value for key,value in changes.items() if value is not None})
    config['enabled'] = True
    parameters(config)
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.NamedTemporaryFile(mode='w', dir=state, delete=False) as out:
        tmp = Path(out.name)
        try:
            json.dump(config, out, indent=2)
            out.write('\n')
            out.close()
            os.replace(tmp, path)
        finally:
            tmp.unlink(missing_ok=True)
    return {'configured': True}


def control(root, action, aide_port=None):
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
    starting = action != 'stop' and config.get('enabled')
    if starting:
        if aide_port is not None:
            config['aide_port'] = aide_port
        args = parameters(config)
        if not python.is_file():
            raise ValueError('Local SIP gateway dependencies are not installed')
        adapter = script.with_name('aide_sip_members.py')
        identity = hashlib.sha256(script.read_bytes()+adapter.read_bytes()+json.dumps(args).encode()).hexdigest()
        if owned and action == 'start' and record.get('identity') == identity:
            return {'running': True, 'pid': pid}
    if owned:
        os.kill(pid, signal.SIGTERM)
        for _ in range(30):
            try: os.kill(pid, 0)
            except ProcessLookupError: break
            time.sleep(.1)
        else:
            raise ValueError('Previous gateway still stopping')
    if not starting:
        return {'running': False}
    args = [str(python), '-u', str(script), *args, '--normal', '--serve',
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
    parser.add_argument('action', choices=['configure', 'start', 'restart', 'stop'])
    parser.add_argument('--root', required=True)
    parser.add_argument('--phone')
    parser.add_argument('--phone-user')
    parser.add_argument('--sip-port', type=int)
    parser.add_argument('--phone-port', type=int)
    parser.add_argument('--aide-port', type=int)
    args = parser.parse_args()
    try:
        if args.action == 'configure':
            result = configure(args.root, {'phone':args.phone, 'phone_user':args.phone_user,
                'port':args.sip_port, 'phone_port':args.phone_port, 'aide_port':args.aide_port})
        else:
            result = control(args.root, args.action, args.aide_port)
        print(json.dumps(result))
    except (OSError, TypeError, ValueError, KeyError) as error:
        print(json.dumps({'running': False, 'error': type(error).__name__}))
        sys.exit(1)
