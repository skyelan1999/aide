#!/usr/bin/env python3
"""Private OpenSSH ProxyCommand: pinned TLS/challenge bridge, then raw SSH."""
import base64
import hashlib
import hmac
import json
import socket
import os
import select
import ssl
import sys


def line(sock):
    data = bytearray()
    while len(data) < 4096:
        piece = sock.recv(1)
        if not piece:
            raise ConnectionError('relay closed')
        if piece == b'\n':
            return json.loads(data)
        data.extend(piece)
    raise ValueError('relay header too large')


def connect(config, probe=False):
    from cryptography import x509
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    key = base64.b64decode(config['key'], validate=True)
    if len(key) != 32:
        raise ValueError('invalid relay key')
    seed = hmac.new(key, b'aide-sftp-relay-certificate-v1', hashlib.sha256).digest()
    expected = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    # The exact key-derived Ed25519 public key is checked before any proof or
    # SSH bytes are sent. A public CA cannot authenticate this private bridge.
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE
    raw = socket.create_connection((config['host'], config['listenPort']), timeout=10)
    try:
        sock = context.wrap_socket(raw, server_hostname=config['host'])
        certificate = x509.load_der_x509_certificate(sock.getpeercert(binary_form=True))
        actual = certificate.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
        if not hmac.compare_digest(expected, actual):
            raise PermissionError('relay identity mismatch')
        hello = line(sock)
        nonce = hello.get('challenge', '')
        challenge = base64.b64decode(nonce, validate=True)
        if hello.get('version') != 1 or len(challenge) != 32 or base64.b64encode(challenge).decode() != nonce:
            raise ValueError('invalid relay handshake')
        mode = 'probe' if probe else 'tunnel'
        digest = config['configuration']
        message = 'aide-sftp-relay-v1:' + nonce + ':' + mode + ':' + config['id'] + ':' + digest
        proof = hmac.new(key, message.encode(), hashlib.sha256).hexdigest()
        sock.sendall(json.dumps({'id':config['id'], 'mode':mode, 'configuration':digest, 'proof':proof}).encode()+b'\n')
        ack = line(sock)
        if not ack.get('ok') or ack.get('kind') != 'sftp' or ack.get('id') != config['id'] or ack.get('configuration') != digest:
            raise PermissionError('relay deployment mismatch')
        sock.settimeout(None)
        return sock
    except BaseException:
        raw.close()
        raise


def main():
    probe = sys.argv[1:] == ['--probe']
    if probe:
        config = json.load(sys.stdin)
    else:
        with open(sys.argv[1], encoding='utf-8') as source:
            config = json.load(source)
    with connect(config, probe) as sock:
        if probe:
            return
        # OpenSSH -f backgrounds the master. A surviving proxy must not keep
        # the launcher's diagnostic pipe open after authentication succeeds.
        null = os.open(os.devnull, os.O_WRONLY)
        os.dup2(null, 2)
        os.close(null)
        while True:
            ready = [sock] if sock.pending() else select.select([sys.stdin.buffer, sock], [], [])[0]
            for source in ready:
                if source is sock:
                    data = sock.recv(65536)
                    if not data: return
                    sys.stdout.buffer.write(data)
                    sys.stdout.buffer.flush()
                else:
                    data = os.read(sys.stdin.fileno(), 65536)
                    if not data: return
                    sock.sendall(data)


if __name__ == '__main__':
    try:
        main()
    except Exception:
        sys.exit('SFTP relay connection failed; check deployment, key and target availability.')
