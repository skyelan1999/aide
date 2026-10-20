#!/usr/bin/env python3
"""Aide target relay. Install: python3 -m pip install cryptography
Serial targets also need: python3 -m pip install pyserial==3.5
This file contains the relay secret. Keep it private. Run manually with python3.
TLS 1.3 protects the link; target addresses are fixed in CONFIG, never from clients.
"""
import base64, datetime, hashlib, hmac, json, os, select, socket, ssl, struct, sys, tempfile, threading
CONFIG = json.loads(__CONFIG_JSON__)

def line(sock):
    out = bytearray()
    while len(out) < 4096:
        byte = sock.recv(1)
        if not byte: raise ConnectionError('closed')
        if byte == b'\n': return json.loads(out)
        out.extend(byte)
    raise ValueError('header too long')
def send_json(sock, value): sock.sendall(json.dumps(value, separators=(',', ':')).encode() + b'\n')
def receive(sock, size):
    out = bytearray()
    while len(out) < size:
        chunk = sock.recv(size-len(out))
        if not chunk: raise ConnectionError('closed')
        out.extend(chunk)
    return bytes(out)
def make_context(directory, key):
    try:
        from cryptography import x509
        from cryptography.hazmat.primitives import serialization
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.x509.oid import NameOID
    except ImportError:
        raise RuntimeError('Install dependency: python3 -m pip install cryptography')
    seed = hmac.new(key, b'aide-terminal-relay-certificate-v1', hashlib.sha256).digest()
    private = Ed25519PrivateKey.from_private_bytes(seed)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'Aide terminal relay')])
    now = datetime.datetime.now(datetime.timezone.utc)
    certificate = x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(private.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=365)).sign(private, None)
    cert_path, key_path = os.path.join(directory,'certificate.pem'), os.path.join(directory,'identity.pem')
    for file, data in [(cert_path,certificate.public_bytes(serialization.Encoding.PEM)),(key_path,private.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))]:
        fd=os.open(file,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb') as output: output.write(data)
    context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version=ssl.TLSVersion.TLSv1_3
    context.load_cert_chain(cert_path,key_path)
    return context

slots=threading.BoundedSemaphore(8)
serial_lock=threading.Lock()
def client(raw, context, key):
    connection=None; target=None; locked=False; started=False
    try:
        raw.settimeout(10)
        connection=context.wrap_socket(raw,server_side=True)
        nonce=base64.b64encode(os.urandom(32)).decode()
        send_json(connection,{'version':1,'challenge':nonce})
        auth=line(connection)
        mode=auth.get('mode')
        message=('aide-relay-v1:'+nonce+':'+str(mode)+':'+CONFIG['id']).encode()
        expected=hmac.new(key,message,hashlib.sha256).hexdigest()
        if mode not in ('probe','open') or auth.get('id')!=CONFIG['id'] or not hmac.compare_digest(str(auth.get('proof','')),expected):
            raise ValueError('authentication failed')
        if mode=='probe':
            send_json(connection,{'ok':True,'id':CONFIG['id'],'kind':CONFIG['kind']});return
        kind=CONFIG['kind']
        if kind=='serial':
            if not serial_lock.acquire(blocking=False): raise RuntimeError('serial port busy')
            locked=True
            try: import serial
            except ImportError: raise RuntimeError('Install dependency: python3 -m pip install pyserial')
            target=serial.Serial(CONFIG['devicePath'],baudrate=CONFIG['baudRate'],bytesize=CONFIG['dataBits'],stopbits=CONFIG['stopBits'],parity={'none':'N','even':'E','odd':'O','mark':'M','space':'S'}[CONFIG['parity']],rtscts=CONFIG['flowControl'],timeout=0.5,write_timeout=10)
        elif kind=='udp':
            target=socket.socket(socket.AF_INET,socket.SOCK_DGRAM)
            target.connect((CONFIG['host'],CONFIG['port']));target.settimeout(0.5)
        else:
            target=socket.create_connection((CONFIG['host'],CONFIG['port']),timeout=10);target.settimeout(None)
        send_json(connection,{'ok':True,'id':CONFIG['id'],'kind':kind})
        started=True
        connection.settimeout(None)
        done=threading.Event()
        def target_to_client():
            try:
                while not done.is_set():
                    try: data=target.read(65536) if kind=='serial' else target.recv(65535)
                    except socket.timeout: continue
                    if not data:
                        if kind=='serial': continue
                        if kind!='udp': break
                    connection.sendall((struct.pack('!I',len(data)) if kind=='udp' else b'')+data)
            except Exception: pass
            finally:
                done.set()
                try: connection.shutdown(socket.SHUT_RDWR)
                except Exception: pass
        reader=threading.Thread(target=target_to_client,daemon=True);reader.start()
        while not done.is_set():
            if kind=='udp':
                size=struct.unpack('!I',receive(connection,4))[0]
                if size>65507: raise ValueError('UDP frame too large')
                data=receive(connection,size);target.send(data)
            else:
                data=connection.recv(65536)
                if not data: break
                if kind=='serial': target.write(data)
                else: target.sendall(data)
        done.set()
    except Exception as error:
        # No addresses, credentials or payloads are logged. Authenticated peers
        # receive a bounded target error before the stream starts.
        if connection and not started:
            try: send_json(connection,{'ok':False,'error':str(error)[:300]})
            except Exception: pass
    finally:
        if target:
            if CONFIG['kind']!='serial':
                try: target.shutdown(socket.SHUT_RDWR)
                except Exception: pass
            try: target.close()
            except Exception: pass
        if connection:
            try: connection.close()
            except Exception: pass
        raw.close()
        if locked: serial_lock.release()
        slots.release()

def main():
    key=base64.b64decode(CONFIG['key'],validate=True)
    if len(key)!=32: raise ValueError('relay key must be 32 bytes')
    with tempfile.TemporaryDirectory(prefix='aide-relay-') as directory:
        context=make_context(directory,key)
        listener=socket.socket();listener.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
        listener.bind((CONFIG['listenHost'],CONFIG['listenPort']));listener.listen(16)
        print('Aide relay ready on TCP port '+str(listener.getsockname()[1]),flush=True)
        try:
            while True:
                raw,_=listener.accept()
                if not slots.acquire(blocking=False): raw.close();continue
                threading.Thread(target=client,args=(raw,context,key),daemon=True).start()
        finally: listener.close()
if __name__=='__main__':
    try: main()
    except KeyboardInterrupt: pass
    except Exception as error: sys.exit(str(error))
