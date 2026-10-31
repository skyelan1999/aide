#!/usr/bin/env python3
"""Aide SFTP relay. Install: python -m pip install cryptography
Run outside any shared directory. This file contains a private relay key.
Forwards only to the embedded SFTP target. SSH authentication remains in Aide.
"""
import base64, datetime, hashlib, hmac, json, os, select, socket, ssl, sys, tempfile, threading
CONFIG = json.loads(__CONFIG_JSON__)
def exact(sock, count):
    data = bytearray()
    while len(data) < count:
        piece = sock.recv(min(65536, count - len(data)))
        if not piece:
            raise ConnectionError('closed')
        data.extend(piece)
    return bytes(data)


def line(sock):
    data = bytearray()
    while len(data) < 4096:
        char = exact(sock, 1)
        if char == b'\n':
            value = json.loads(data)
            if not isinstance(value, dict):
                raise ValueError('invalid header')
            return value
        data.extend(char)
    raise ValueError('header too large')


def send(sock, value):
    sock.sendall(json.dumps(value, separators=(',', ':')).encode() + b'\n')


def make_context(directory, key):
    try:
        from cryptography import x509
        from cryptography.hazmat.primitives import serialization
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.x509.oid import NameOID
    except ImportError:
        raise RuntimeError('Install dependency: python -m pip install cryptography')
    seed = hmac.new(key, b'aide-sftp-relay-certificate-v1', hashlib.sha256).digest()
    private = Ed25519PrivateKey.from_private_bytes(seed)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'Aide SFTP reference relay')])
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(private.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=365)).sign(private, None)
    for filename, data in [('certificate.pem', certificate.public_bytes(serialization.Encoding.PEM)), ('identity.pem', private.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))]:
        fd = os.open(os.path.join(directory, filename), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as output:
            output.write(data)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(os.path.join(directory, 'certificate.pem'), os.path.join(directory, 'identity.pem'))
    return context



slots = threading.BoundedSemaphore(8)

def forward(sock, upstream):
    while True:
        ready = [sock] if sock.pending() else select.select([sock,upstream],[],[],60)[0]
        if not ready: return
        for source in ready:
            data = source.recv(65536)
            if not data: return
            (upstream if source is sock else sock).sendall(data)

def client(raw, context, key):
    try:
        raw.settimeout(15)
        with context.wrap_socket(raw, server_side=True) as sock:
            nonce = base64.b64encode(os.urandom(32)).decode()
            send(sock, {'version':1, 'challenge':nonce})
            auth = line(sock)
            mode = auth.get('mode')
            digest = CONFIG['configuration']
            message = 'aide-sftp-relay-v1:' + nonce + ':' + str(mode) + ':' + CONFIG['id'] + ':' + digest
            proof = hmac.new(key, message.encode(), hashlib.sha256).hexdigest()
            if mode not in ('probe','tunnel') or auth.get('id') != CONFIG['id'] or auth.get('configuration') != digest or not hmac.compare_digest(str(auth.get('proof','')),proof):
                raise PermissionError('relay authentication failed')
            with socket.create_connection((CONFIG['targetHost'], CONFIG['targetPort']), timeout=10) as upstream:
                send(sock, {'ok':True, 'id':CONFIG['id'], 'kind':'sftp', 'configuration':digest})
                if mode == 'probe': return
                # SSH keepalive is every 15 seconds; abandoned sessions expire.
                sock.settimeout(60); upstream.settimeout(60)
                forward(sock,upstream)
    except (OSError, ValueError, PermissionError):
        raw.close()
    finally:
        slots.release()

def main():
    key = base64.b64decode(CONFIG['key'],validate=True)
    if len(key) != 32: raise ValueError('invalid relay key')
    with tempfile.TemporaryDirectory(prefix='aide-sftp-relay-') as directory:
        context = make_context(directory,key)
        with socket.socket(socket.AF_INET6 if ':' in CONFIG['listenHost'] else socket.AF_INET) as listener:
            listener.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
            listener.bind((CONFIG['listenHost'], CONFIG['listenPort'])); listener.listen(16)
            print('Aide SFTP relay ready on TCP port ' + str(listener.getsockname()[1]),flush=True)
            while True:
                raw,_ = listener.accept()
                if not slots.acquire(blocking=False): raw.close(); continue
                threading.Thread(target=client,args=(raw,context,key),daemon=True).start()

if __name__ == '__main__':
    try: main()
    except KeyboardInterrupt: pass
    except Exception: sys.exit('SFTP relay failed. Check cryptography, listening port and target configuration.')
