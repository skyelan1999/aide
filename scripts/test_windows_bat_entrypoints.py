#!/usr/bin/env python3
"""Run actual root BATs via Windows CMD; native/Docker boundaries stay disposable."""
import os
from pathlib import Path
import shutil
import subprocess
import time

ROOT=Path(__file__).resolve().parents[1]
ACTIONS=('start','stop','build','clean','docker_clean','verify','release','clean-docker-cache')


def check(binary, folder, installation):
    def fixture(name, source=True, exe=binary):
        root=folder/('BAT '+name+' \u4e2d\u6587 space');root.mkdir();(root/'scripts').mkdir()
        for action in ACTIONS:shutil.copy2(ROOT/(action+'.bat'),root)
        for script in ('windows-entrypoint.bat','windows-bootstrap.bat'):shutil.copy2(ROOT/'scripts'/script,root/'scripts')
        if source:
            (root/'go.mod').touch();(root/'Dockerfile').touch()
        if exe:shutil.copy2(exe,root/'aide-launcher.exe')
        return root

    def run(root,action,args='--dry-run',env=None,expected=0):
        driver=folder/'driver.cmd'
        driver.write_bytes(('@echo off\r\ncall "'+str(root/(action+'.bat'))+'" '+args+'\r\nexit /b %ERRORLEVEL%\r\n').encode('utf-8'))
        # Set UTF-8 before CMD parses the path containing Chinese characters.
        command='chcp 65001 >nul & call "'+str(driver)+'"'
        result=subprocess.run(['cmd.exe','/d','/c',command],input='',capture_output=True,encoding='utf-8',errors='replace',
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
    for name,define in (('new',''),('old','/DFIXTURE_LEGACY'),('docker','/DFIXTURE_DOCKER')):
        lines+=['cl /nologo /utf-8 /std:c++17 /EHsc '+define+' "'+str(fixture_source)+'" /Fe:'+name+'.exe','if errorlevel 1 exit /b 1']
    build.write_bytes(('\r\n'.join(lines)+'\r\n').encode('utf-8'))
    subprocess.run(['cmd.exe','/d','/c',str(build)],cwd=folder,check=True)
    toolbin=folder/'fixture-tools';toolbin.mkdir();shutil.copy2(folder/'docker.exe',toolbin/'docker.exe')
    calls=folder/'calls.log'
    env=dict(os.environ,AIDE_LAUNCHER_NO_PAUSE='1',FIXTURE_CALLS=str(calls),FIXTURE_NEW_EXE=str(folder/'new.exe'),
        PATH=str(toolbin)+os.pathsep+str(Path(os.environ['SystemRoot'])/'System32'))
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
    run(current,'verify',args='',env=dict(env,FIXTURE_ACTION_FAIL='1'),expected=23)
    run(current,'clean-docker-cache',args='',env=env)

    # Explorer-style CMD /c must remain open at PAUSE until input arrives.
    driver=folder/'interactive.cmd';driver.write_bytes(('@echo off\r\ncall "'+str(current/'stop.bat')+'"\r\nexit /b %ERRORLEVEL%\r\n').encode('utf-8'))
    interactive=dict(env);interactive.pop('AIDE_LAUNCHER_NO_PAUSE')
    output=folder/'interactive-output.txt'
    with output.open('wb') as capture:
        process=subprocess.Popen(['cmd.exe','/d','/c','chcp 65001 >nul & call "'+str(driver)+'"'],stdin=subprocess.PIPE,stdout=capture,stderr=subprocess.STDOUT,env=interactive)
        try:
            deadline=time.monotonic()+10
            while time.monotonic()<deadline and b'finished with exit code' not in output.read_bytes():time.sleep(.05)
            assert b'finished with exit code 0' in output.read_bytes(),output.read_bytes()
            assert process.poll() is None,'Double-click console closed before acknowledgement'
            process.communicate(b'\n',timeout=10);assert process.returncode==0
        finally:
            if process.poll() is None:process.kill();process.wait()
    print('PASS actual BAT/CMD: source/release, missing/stale EXE, fresh bootstrap success/failure, preserved data/exit code and interactive pause')
