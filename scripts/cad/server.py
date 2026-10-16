#!/usr/bin/env python3
"""Private, authenticated, single-job CAD worker; no user files are mounted."""
import hmac
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LIMIT = 384 << 20
GATE = threading.BoundedSemaphore(1)
TOKEN_FILE = os.environ.get('AIDE_CAD_TOKEN_FILE', '/cad-auth/token')

class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *args):
        pass  # Neither user paths nor credentials belong in worker logs.
    def reject(self, code, message):
        body = json.dumps({'error': message}, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.send_header('Connection', 'close')
        self.end_headers()
        self.wfile.write(body)
        self.close_connection = True
    def do_POST(self):
        try:
            token = Path(TOKEN_FILE).read_text().strip()
        except OSError:
            return self.reject(503, 'CAD 服务尚未就绪。')
        if not token or not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer '+token):
            return self.reject(401, 'CAD 服务认证失败。')
        if self.path != '/convert':
            return self.reject(404, '未知 CAD 接口。')
        if not GATE.acquire(blocking=False):
            return self.reject(429, '已有 CAD 模型正在转换，请等待或取消后重试。')
        proc = None
        try:
            self.connection.settimeout(30)
            with tempfile.TemporaryDirectory(prefix='aide-cad-') as directory:
                source = Path(directory)/'model.step'
                size = 0
                with source.open('wb') as target:
                    if self.headers.get('Transfer-Encoding', '').lower() == 'chunked':
                        while True:
                            line = self.rfile.readline(65)
                            if len(line) > 64 or not line.endswith(b'\r\n'):
                                raise ValueError('CAD 传输块格式无效。')
                            count = int(line.strip(), 16)
                            if count == 0:
                                if self.rfile.read(2) != b'\r\n':
                                    raise ValueError('CAD 传输结尾无效。')
                                break
                            if count < 0 or size+count > LIMIT:
                                raise ValueError('STEP 文件超过 384 MiB 预览上限。')
                            remaining = count
                            while remaining:
                                chunk = self.rfile.read(min(65536, remaining))
                                if not chunk:
                                    raise ValueError('STEP 文件传输未完成。')
                                target.write(chunk)
                                size += len(chunk)
                                remaining -= len(chunk)
                            if self.rfile.read(2) != b'\r\n':
                                raise ValueError('CAD 传输块格式无效。')
                    else:
                        size = int(self.headers.get('Content-Length', '0'))
                        if not 0 < size <= LIMIT:
                            raise ValueError('STEP 文件大小无效或超过 384 MiB。')
                        remaining = size
                        while remaining:
                            chunk = self.rfile.read(min(65536, remaining))
                            if not chunk:
                                raise ValueError('STEP 文件传输未完成。')
                            target.write(chunk)
                            remaining -= len(chunk)
                if not size:
                    raise ValueError('STEP 文件为空。')
                self.send_response(200)
                self.send_header('Content-Type', 'application/x-ndjson')
                self.send_header('Cache-Control', 'no-store')
                self.send_header('Connection', 'close')
                self.end_headers()
                self.close_connection = True
                self.connection.settimeout(15)
                proc = subprocess.Popen([sys.executable, '-u', str(Path(__file__).with_name('convert.py')), str(source)], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, start_new_session=True)
                started = heartbeat = time.monotonic()
                pending = b''
                with selectors.DefaultSelector() as selector:
                    selector.register(proc.stdout, selectors.EVENT_READ)
                    while True:
                        now = time.monotonic()
                        if now-started > 900:
                            self.wfile.write((json.dumps({'error':'CAD 转换超时，转换进程已终止。'})+'\n').encode())
                            self.wfile.flush()
                            break
                        if now-heartbeat >= 1:
                            self.wfile.write(b'{"kind":"heartbeat"}\n')
                            self.wfile.flush()
                            heartbeat = now
                        if not selector.select(.5):
                            continue
                        chunk = os.read(proc.stdout.fileno(), 65536)
                        if not chunk:
                            if pending or proc.wait() != 0:
                                self.wfile.write(b'{"error":"CAD conversion failed; incomplete geometry discarded."}\n')
                                self.wfile.flush()
                            break
                        pending += chunk
                        while b'\n' in pending:
                            line, pending = pending.split(b'\n', 1)
                            if len(line) > 4 << 20:
                                raise ValueError('CAD 输出块超过安全预算。')
                            self.wfile.write(line+b'\n')
                            self.wfile.flush()
                        if len(pending) > 4 << 20:
                            raise ValueError('CAD 输出块超过安全预算。')
        except (BrokenPipeError, ConnectionError, TimeoutError):
            pass
        except (OSError, ValueError) as exc:
            if proc is None:
                self.reject(400, str(exc))
            else:
                try:
                    self.wfile.write((json.dumps({'error': str(exc)})+'\n').encode())
                    self.wfile.flush()
                except OSError:
                    pass
        finally:
            if proc is not None:
                if proc.poll() is None:
                    os.killpg(proc.pid, signal.SIGTERM)
                    try:
                        proc.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        os.killpg(proc.pid, signal.SIGKILL)
                        proc.wait()
                proc.stdout.close()
            GATE.release()

if __name__ == '__main__':
    # Only our disposable, unmounted upload files may be removed after a crash.
    import shutil
    for child in Path(tempfile.gettempdir()).glob('aide-cad-*'):
        if child.is_dir() and not child.is_symlink():
            shutil.rmtree(child)
    server = ThreadingHTTPServer(('0.0.0.0', int(os.environ.get('AIDE_CAD_PORT', '8082'))), Handler)
    server.daemon_threads = True
    server.serve_forever()
