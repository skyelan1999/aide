#!/usr/bin/env python3
"""Real WebView2 isolated UI check on Windows; no Docker/user credentials/Hello prompts."""
from pathlib import Path
import json
import os
import platform
import subprocess
import tempfile
import time
ROOT=Path(__file__).resolve().parents[2]
def main():
 if platform.system()!='Windows':raise SystemExit('NOT_RUN: this check requires real Windows and WebView2')
 artifacts=ROOT/'.agent-state/windows-desktop';artifacts.mkdir(parents=True,exist_ok=True)
 with tempfile.TemporaryDirectory(prefix='Aide desktop 中文 空格 ') as directory:
  base=Path(directory);ready=base/'ready.json'
  server=subprocess.Popen([str(artifacts/'fixture.exe'),'--ready',str(ready)],stdout=subprocess.DEVNULL)
  try:
   for _ in range(100):
    if ready.exists():break
    if server.poll() is not None:raise RuntimeError('Synthetic TLS fixture failed')
    time.sleep(.1)
   if not ready.exists():raise RuntimeError('TLS fixture readiness timeout')
   with (artifacts/'native-ui.log').open('w',encoding='utf-8') as log:
    subprocess.run([str(artifacts/'desktop-smoke.exe'),str(ready),str(base/'profile'),str(artifacts/'WebView2Loader.dll'),str(artifacts/'native-ui.json')],stdout=log,stderr=subprocess.STDOUT,check=True,timeout=120)
   result=json.loads((artifacts/'native-ui.json').read_text())
   assert result['ok'] is True,result
   print(json.dumps(result,indent=2))
  finally:
   server.terminate();server.wait(timeout=10)
if __name__=='__main__':main()
