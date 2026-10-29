#!/usr/bin/env python3
"""Private CPU embedding sidecar. Run in Aide's network namespace, no published ports."""
import hmac
import json
import os
import socket
from http.server import BaseHTTPRequestHandler, HTTPServer

MODEL = 'sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2'


def main():
    import torch
    from sentence_transformers import SentenceTransformer
    torch.set_num_threads(2)
    token = os.environ['MIXSOUL_EMBEDDING_TOKEN']
    if len(token) != 64 or any(c not in '0123456789abcdef' for c in token):
        raise ValueError('Invalid service token')
    encoder = SentenceTransformer('/models', device='cpu', local_files_only=True,
                                  trust_remote_code=False)

    class Handler(BaseHTTPRequestHandler):
        def setup(self):
            super().setup()
            self.connection.settimeout(8)

        def reply(self, status, data):
            raw = json.dumps(data).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(raw)))
            self.send_header('Cache-Control', 'no-store')
            self.end_headers()
            self.wfile.write(raw)

        def do_POST(self):
            if not hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + token):
                self.reply(401, {'error': 'unauthorized'})
                return
            try:
                if self.path != '/v1/embeddings' or self.headers.get('Transfer-Encoding'):
                    raise ValueError()
                length = int(self.headers.get('Content-Length', '0'))
                if not 0 < length <= 262144:
                    raise ValueError()
                body = json.loads(self.rfile.read(length))
                texts = body.get('input')
                if body.get('model') != MODEL or not isinstance(texts, list) or not 1 <= len(texts) <= 16:
                    raise ValueError()
                if any(not isinstance(s, str) or len(s) > 4000 for s in texts):
                    raise ValueError()
                vectors = encoder.encode(texts, normalize_embeddings=True, show_progress_bar=False).tolist()
                self.reply(200, {'model': MODEL, 'data': [
                    {'index': i, 'embedding': v} for i, v in enumerate(vectors)]})
            except (ValueError, KeyError, TypeError, socket.timeout):
                self.reply(400, {'error': 'invalid request'})
            except Exception:
                self.reply(503, {'error': 'embedding unavailable'})

        def log_message(self, *args):
            pass  # Never log request text or credentials.

    server = HTTPServer(('127.0.0.1', 32141), Handler)
    print('MIXSoul embedding ready: loopback:32141, dimensions=384', flush=True)
    server.serve_forever()


if __name__ == '__main__':
    main()
