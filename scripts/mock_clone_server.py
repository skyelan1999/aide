#!/usr/bin/env python3
"""mock_clone_server.py — 仿自托管音色克隆服务（#36 端到端测试用）。

与 scripts/mock_provider.py（LLM mock，:9001）互补：本脚本只仿克隆 TTS 服务。

端点（对齐 internal/server/tts/clone.go 的适配层）：
  POST /audio/speech   OpenAI 兼容：{model,input,voice,response_format,speed} → audio/mpeg
  POST /voices         创建音色：{sampleIds,backend,...} → {"voice_id":"mock-voice-001","status":"ready"}
  GET  /tts            GPT-SoVITS 兼容：?text=&text_lang=&text_id= → audio/wav
  GET  /healthz        存活探针

用法：
  python3 scripts/mock_clone_server.py --port 9880
  # aide 设置里填 CloneTTSBaseURL=http://127.0.0.1:9880，TTSProvider=clone
  # 录音 → 上传加密样本 → 创建音色 → 用该音色合成可播放

真实克隆模型（GPT-SoVITS/IndexTTS2/CosyVoice2/OpenVoice v2）效果 NOT_RUN；
本 mock 只验证"上传→加密→创建音色→合成"链路与降级逻辑。
"""
from __future__ import annotations

import argparse
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# 最小可识别 MP3 帧（静音）；Content-Type=audio/mpeg 合法，<audio> 能播。
FAKE_MP3 = bytes.fromhex(
    "fffb900000000000000000000000000000000000"
    "0000000000000000000000000000000000000000"
) * 8

FAKE_WAV = (
    b"RIFF" + (36).to_bytes(4, "little") + b"WAVE"
    + b"fmt " + (16).to_bytes(4, "little")
    + (1).to_bytes(2, "little")   # PCM
    + (1).to_bytes(2, "little")    # mono
    + (16000).to_bytes(4, "little")
    + (32000).to_bytes(4, "little")
    + (2).to_bytes(2, "little")
    + (16).to_bytes(2, "little")
    + b"data" + (8000).to_bytes(4, "little")
    + b"\x00" * 8000
)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # 安静
        pass

    def _json(self, code, obj):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _audio(self, code, data, ctype):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/healthz":
            self._json(200, {"ok": True, "service": "mock-clone"})
        elif path == "/tts":
            self._audio(200, FAKE_WAV, "audio/wav")
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        path = self.path.split("?")[0]
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length else b""
        if path == "/audio/speech":
            try:
                req = json.loads(body.decode("utf-8")) if body else {}
            except Exception:
                req = {}
            print(f"[mock-clone] /audio/speech voice={req.get('voice')!r} "
                  f"input={req.get('input','')[:30]!r} speed={req.get('speed')}",
                  file=sys.stderr, flush=True)
            time.sleep(0.05)
            self._audio(200, FAKE_MP3, "audio/mpeg")
        elif path == "/voices":
            try:
                req = json.loads(body.decode("utf-8")) if body else {}
            except Exception:
                req = {}
            n = len(req.get("sampleIds", []) or req.get("samples", []))
            print(f"[mock-clone] /voices creating voice from {n} samples",
                  file=sys.stderr, flush=True)
            self._json(200, {"voice_id": "mock-voice-001", "status": "ready",
                              "samples": n})
        else:
            self._json(404, {"error": "not found"})


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9880)
    args = ap.parse_args()
    srv = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(f"mock-clone listening on http://127.0.0.1:{args.port}", flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
