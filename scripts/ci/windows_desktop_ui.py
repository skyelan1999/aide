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
 temporary=tempfile.TemporaryDirectory(prefix='Aide desktop 中文 空格 ')
 try:
  base=Path(temporary.name);ready=base/'ready.json'
  with (artifacts/'production-self-test.log').open('w',encoding='utf-8') as log:
   subprocess.run([str(artifacts/'aide-launcher.exe'),'--self-test'],stdout=log,stderr=subprocess.STDOUT,check=True,timeout=30)
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
 finally:
  # WebView2 closes its browser process asynchronously after controllers close.
  # Retry only this owned synthetic profile; never kill other Edge/Aide processes.
  for attempt in range(100):
   try:temporary.cleanup();break
   except OSError:
    if attempt==99:raise
    time.sleep(.2)
if __name__=='__main__':main()
