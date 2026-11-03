#!/usr/bin/env python3
"""Real WebView2 isolated UI check on Windows; no Docker/user credentials/Hello prompts."""
from pathlib import Path
import json
import ctypes
import hashlib
import shutil
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
   # Exercise production open -> hidden desktop child -> native window, including
   # the compiled-launcher delegation used after a real source build.
   install=base/'production 源码';install.mkdir();toolbin=base/'tools';toolbin.mkdir()
   shutil.copyfile(artifacts/'fixture.exe',toolbin/'docker.exe')
   executable=install/'aide-launcher.exe';shutil.copyfile(artifacts/'aide-launcher.exe',executable)
   digest=hashlib.sha256(executable.read_bytes()).hexdigest();native=install/'.aide-data/native';compiled=native/'compiled';compiled.mkdir(parents=True)
   cached=compiled/('launcher-'+digest+'.exe');shutil.copyfile(executable,cached)
   (native/'compiled-launcher.json').write_text(json.dumps({'version':1,'file':cached.name,'sha256':digest}),encoding='utf-8')
   (install/'.env').write_text('COMPOSE_PROJECT_NAME=aide-fixture\n',encoding='utf-8')
   env=dict(os.environ,PATH=str(toolbin)+os.pathsep+os.environ['PATH'],AIDE_FIXTURE_READY=str(ready),AIDE_LAUNCHER_NO_PAUSE='1')
   env.pop('AIDE_OPEN_BROWSER',None);env.pop('COMPOSE_FILE',None)
   observed=base/'ui-ready';observed.unlink(missing_ok=True)
   with (artifacts/'production-startup.log').open('w',encoding='utf-8') as log:
    subprocess.run([str(executable),'open','--root',str(install)],cwd=install,env=env,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=90)
   for _ in range(200):
    if observed.exists():break
    time.sleep(.1)
   assert observed.exists(),'Production child did not render the fixture'
   identity=native/'desktop/identity';key=hashlib.sha256(identity.read_bytes()).hexdigest()
   ctypes.windll.user32.FindWindowW.argtypes=[ctypes.c_wchar_p,ctypes.c_wchar_p];ctypes.windll.user32.FindWindowW.restype=ctypes.c_void_p
   ctypes.windll.user32.IsWindowVisible.argtypes=[ctypes.c_void_p]
   hwnd=ctypes.windll.user32.FindWindowW('AideDesktop_'+key,None)
   assert hwnd and ctypes.windll.user32.IsWindowVisible(hwnd),'Production window is missing'
   assert (native/'desktop.log').exists() and (native/'launcher.log').exists()
   subprocess.run([str(executable),'desktop-stop','--root',str(install)],cwd=install,env=env,check=True,timeout=30)
   conflict=subprocess.run([str(executable),'start','stop','--dry-run','--root',str(install)],env=env,capture_output=True,timeout=15)
   assert conflict.returncode!=0 and b'Conflicting launcher actions' in conflict.stderr
   report={'ok':True,'hiddenChildWindow':True,'compiledReceiptDelegation':True,'productionPageRendered':True,'startupLogsExist':True,'conflictingStartStopRejected':True,'limits':'Disposable Docker transport, no real Docker Desktop or user account'}
   (artifacts/'production-startup.json').write_text(json.dumps(report,indent=2))
   # Only the profile identified by this owned temporary installation is removed.
   profile=Path(os.environ['LOCALAPPDATA'])/'Aide/Desktop'/key
   for attempt in range(100):
    try:
     if profile.exists():shutil.rmtree(profile)
     break
    except OSError:
     if attempt==99:raise
     time.sleep(.2)

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
