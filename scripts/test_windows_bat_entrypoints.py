#!/usr/bin/env python3
"""Run actual root BATs via Windows CMD; native/Docker boundaries stay disposable."""
import os
import ctypes
import json
from pathlib import Path
import shutil
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]
ACTIONS=('start','stop','build','clean','docker_clean','verify','release','clean-docker-cache')

def cmd(command):
    # list2cmdline targets the C runtime; CMD does not unescape its backslash quotes.
    return 'cmd.exe /d /s /c "'+command+'"'


def check(binary, folder, installation):
    def fixture(name, source=True, exe=binary):
        root=folder/('BAT '+name+' \u4e2d\u6587 space');root.mkdir();(root/'scripts').mkdir()
        for action in ACTIONS:shutil.copy2(ROOT/(action+'.bat'),root)
        for script in ('windows-entrypoint.bat','windows-bootstrap.bat','windows-build-proxy.bat'):shutil.copy2(ROOT/'scripts'/script,root/'scripts')
        if source:
            (root/'go.mod').touch();(root/'Dockerfile').touch()
        if exe:shutil.copy2(exe,root/'aide-launcher.exe')
        return root

    def run(root,action,args='--dry-run',env=None,expected=0):
        driver=folder/'driver.cmd'
        driver.write_bytes(('@echo off\r\ncall "'+str(root/(action+'.bat'))+'" '+args+'\r\nexit /b %ERRORLEVEL%\r\n').encode('utf-8'))
        # Set UTF-8 before CMD parses the path containing Chinese characters.
        command='chcp 65001 >nul & call "'+str(driver)+'"'
        result=subprocess.run(cmd(command),input='',capture_output=True,encoding='utf-8',errors='replace',
            timeout=30,env=env or dict(os.environ,AIDE_LAUNCHER_NO_PAUSE='1'))
        assert result.returncode==expected,(action,expected,result.returncode,result.stdout,result.stderr)
        assert '[Aide]' in result.stdout and 'finished with exit code' in result.stdout,(action,result.stdout)
        return result

    # Real production Win32 EXE through the actual BAT/dispatcher, no Docker.
    for source in (False,True):
        root=fixture('source' if source else 'release',source)
        for action in ACTIONS:
            if action=='clean-docker-cache':continue # This legacy alias has no public dry-run contract.
            allowed=source or action in ('start','stop','docker_clean','verify')
            run(root,action,expected=0 if allowed else 1)
        assert not (root/'.aide-data').exists(),'BAT dry run changed private runtime data'
    missing=fixture('missing',exe=None)
    for action in ('start','stop','docker_clean','verify','release'):
        result=run(missing,action,expected=1);assert 'Missing aide-launcher.exe' in result.stdout

    # Compile tiny native boundaries; they only operate on this test's paths.
    build=folder/'fixture-build.cmd'
    fixture_source=ROOT/'scripts/fixtures/windows-entrypoint.cpp'
    lines=['@echo off','call "'+installation+'\\Common7\\Tools\\VsDevCmd.bat" -arch=x64']
    for name,define in (('new',''),('old','/DFIXTURE_LEGACY'),('docker','/DFIXTURE_DOCKER'),('reg','/DFIXTURE_REG')):
        lines+=['cl /nologo /utf-8 /std:c++17 /EHsc '+define+' "'+str(fixture_source)+'" /Fe:'+name+'.exe','if errorlevel 1 exit /b 1']
    build.write_bytes(('\r\n'.join(lines)+'\r\n').encode('utf-8'))
    subprocess.run(cmd('call "'+str(build)+'"'),cwd=folder,check=True)
    toolbin=folder/'fixture-tools';toolbin.mkdir();shutil.copy2(folder/'docker.exe',toolbin/'docker.exe')
    shutil.copy2(folder/'reg.exe',toolbin/'reg.exe')
    from test_windows_build_proxy import check_cmd
    check_cmd(folder, toolbin)
    calls=folder/'calls.log'
    env=dict(os.environ,AIDE_LAUNCHER_NO_PAUSE='1',FIXTURE_CALLS=str(calls),FIXTURE_NEW_EXE=str(folder/'new.exe'),
        PATH=str(toolbin)+os.pathsep+str(Path(os.environ['SystemRoot'])/'System32'))
    # The actual root build BAT must carry the resolved environment through
    # Docker bootstrap, temporary host-stop and the second native build phase.
    for name,explicit,system,expected in (
        ('direct',{}, {},{}),
        ('system',{},dict(FIXTURE_PROXY_ENABLE='1',FIXTURE_PROXY_SERVER='127.0.0.1:18797'),dict(HTTP_PROXY='http://127.0.0.1:18797',HTTPS_PROXY='http://127.0.0.1:18797')),
        ('explicit',dict(HTTPS_PROXY='http://user:synthetic-secret@proxy.example:18800',NO_PROXY='keep.local'),dict(FIXTURE_PROXY_ENABLE='1',FIXTURE_PROXY_SERVER='ignored.example:80'),dict(HTTPS_PROXY='http://user:synthetic-secret@proxy.example:18800',NO_PROXY='keep.local')),
    ):
        root=fixture('proxy-'+name,exe=folder/'old.exe');calls.write_text('')
        environment={k:v for k,v in env.items() if k.upper() not in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY')}
        environment.update(explicit);environment.update(system)
        environment.update(FIXTURE_CHECK_PROXY='1',FIXTURE_EXPECT_HTTP=expected.get('HTTP_PROXY',''),
                           FIXTURE_EXPECT_HTTPS=expected.get('HTTPS_PROXY',''),FIXTURE_EXPECT_NO=expected.get('NO_PROXY',''))
        result=run(root,'build',args='',env=environment)
        log=calls.read_text();assert 'native:host-stop' in log and 'native:build' in log and 'proxy:wrong' not in log,log
        assert log.count('proxy:inherited')>=4,log
        assert 'synthetic-secret' not in result.stdout+result.stderr
    # Real host-stop from a new EXE must wait for an older, different EXE.
    root=fixture('previous-controller');state=root/'.aide-data/native';state.mkdir(parents=True)
    old_controller=state/'previous-controller.exe';shutil.copy2(folder/'new.exe',old_controller);calls.write_text('')
    previous=subprocess.Popen([str(old_controller),'--wait-stop',str(root)],env=env)
    try:
        deadline=time.monotonic()+10
        while time.monotonic()<deadline and 'previous:started' not in calls.read_text():time.sleep(.05)
        from ctypes import wintypes
        kernel=ctypes.WinDLL('kernel32',use_last_error=True)
        kernel.GetProcessTimes.argtypes=[wintypes.HANDLE]+[ctypes.POINTER(wintypes.FILETIME)]*4
        created,exited,kernel_time,user_time=[wintypes.FILETIME() for _ in range(4)]
        assert kernel.GetProcessTimes(wintypes.HANDLE(int(previous._handle)),ctypes.byref(created),ctypes.byref(exited),ctypes.byref(kernel_time),ctypes.byref(user_time))
        identity={'version':1,'root':str(root.resolve()),'pid':previous.pid,'created':(created.dwHighDateTime<<32)|created.dwLowDateTime,'state':'running'}
        (state/'host-status.json').write_text(json.dumps(identity),encoding='utf-8')
        result=subprocess.run([str(binary),'host-stop','--root',str(root)],capture_output=True,text=True,timeout=20,env=env)
        assert result.returncode==0,(result.stdout,result.stderr)
        assert previous.poll()==0,'host-stop returned while the previous EXE was still alive'
    finally:
        if previous.poll() is None:previous.kill();previous.wait()
    old=fixture('stale',exe=folder/'old.exe')
    result=run(old,'verify',env=env,expected=1);assert 'outdated' in result.stdout
    assert 'legacy:verify' not in calls.read_text()
    for action in ('build','clean'):
        for failure in (True,False):
            root=fixture(action+str(failure),exe=folder/'old.exe')
            data=root/'.aide-data/storage/data';data.mkdir(parents=True);(data/'sentinel').write_bytes(b'preserved')
            calls.write_text('')
            result=run(root,action,args='',env=dict(env,FIXTURE_BUILD_FAIL='1' if failure else '0'),expected=1 if failure else 0)
            log=calls.read_text()
            assert 'docker:buildx:build' in log and 'legacy:'+action not in log,log
            assert (data/'sentinel').read_bytes()==b'preserved'
            if failure:
                assert 'native:'+action not in log and (root/'aide-launcher.exe').read_bytes()==(folder/'old.exe').read_bytes()
            else:
                assert 'native:host-stop' in log and 'native:'+action in log,log
                assert (root/'aide-launcher.exe').read_bytes()==(folder/'new.exe').read_bytes()
    current=fixture('failure-code',exe=folder/'new.exe')
    for invalid in ('short','nonhex','extra'):
        root=fixture('invalid-cid-'+invalid,exe=folder/'old.exe');calls.write_text('')
        run(root,'build',args='',env=dict(env,FIXTURE_CID_BAD=invalid),expected=1)
        assert 'docker:cp:' not in calls.read_text() and 'native:build' not in calls.read_text()
        assert (root/'aide-launcher.exe').read_bytes()==(folder/'old.exe').read_bytes()
        assert not (root/'injected').exists()
    run(current,'verify',args='',env=dict(env,FIXTURE_ACTION_FAIL='1'),expected=23)
    run(current,'clean-docker-cache',args='',env=env)

    # Explorer-style CMD /c must remain open at PAUSE until input arrives.
    driver=folder/'interactive.cmd';driver.write_bytes(('@echo off\r\ncall "'+str(current/'stop.bat')+'"\r\nexit /b %ERRORLEVEL%\r\n').encode('utf-8'))
    interactive=dict(env);interactive.pop('AIDE_LAUNCHER_NO_PAUSE')
    output=folder/'interactive-output.txt'
    # PAUSE reads a console, not a pipe. Create an owned console exactly as
    # Explorer does, redirect only output, and acknowledge through CONIN$.
    process=subprocess.Popen(cmd('chcp 65001 >nul & call "'+str(driver)+'" >"'+str(output)+'" 2>&1'),
        creationflags=subprocess.CREATE_NEW_CONSOLE,env=interactive)
    try:
        deadline=time.monotonic()+10
        while time.monotonic()<deadline:
            if output.exists() and b'finished with exit code' in output.read_bytes():break
            time.sleep(.05)
        assert b'finished with exit code 0' in output.read_bytes(),output.read_bytes()
        time.sleep(.1)
        assert process.poll() is None,('Double-click console closed before acknowledgement',output.read_bytes())
        subprocess.run([str(folder/'new.exe'),'--ack-console',str(process.pid)],check=True,env=env,timeout=10)
        process.wait(timeout=10);assert process.returncode==0
    finally:
        if process.poll() is None:process.kill();process.wait()
    print('PASS actual BAT/CMD: source/release, missing/stale EXE, fresh bootstrap success/failure, preserved data/exit code and interactive pause')
