"""Shared policy cases for the production C++ resolver and real Windows CMD.

Proxy URLs are synthetic. No system proxy/registry/network settings are changed.
"""

def case(name, system=None, env=None, additions=None, source='system', warning=False, success=True):
    return dict(name=name, input=dict(system=system or {}, env=env or {}), success=success,
                expected=dict(source=source, additions=additions or {}, warning=warning))


CASES = [
    case('direct', source='direct'),
    case('disabled-stale-setting', dict(server='127.0.0.1:9999'), source='direct'),
    case('single-manual', dict(enabled=True, server='127.0.0.1:18797'),
         additions=dict(HTTP_PROXY='http://127.0.0.1:18797', HTTPS_PROXY='http://127.0.0.1:18797')),
    case('per-protocol', dict(enabled=True, server='http=proxy.example:80; https=https://secure.example:8443'),
         additions=dict(HTTP_PROXY='http://proxy.example:80', HTTPS_PROXY='https://secure.example:8443')),
    case('manual-spaces', dict(enabled=True, server=' http = proxy.example:80 ; https = secure.example:8443 '),
         additions=dict(HTTP_PROXY='http://proxy.example:80', HTTPS_PROXY='http://secure.example:8443')),
    case('ipv6', dict(enabled=True, server='[::1]:18798'),
         additions=dict(HTTP_PROXY='http://[::1]:18798', HTTPS_PROXY='http://[::1]:18798')),
    case('http-only', dict(enabled=True, server='http=proxy.example:80'), additions=dict(HTTP_PROXY='http://proxy.example:80')),
    case('https-only', dict(enabled=True, server='https=proxy.example:80'), additions=dict(HTTPS_PROXY='http://proxy.example:80')),
    case('ignore-other-protocol', dict(enabled=True, server='socks=127.0.0.1:1080;https=proxy.example:80'),
         additions=dict(HTTPS_PROXY='http://proxy.example:80')),
    case('bypass', dict(enabled=True, server='proxy.example:80', bypass='localhost;*.example.com;10.0.0.0/8;[::1]'),
         additions=dict(HTTP_PROXY='http://proxy.example:80', HTTPS_PROXY='http://proxy.example:80', NO_PROXY='localhost,*.example.com,10.0.0.0/8,[::1]')),
    case('short-host-bypass-warning', dict(enabled=True, server='proxy.example:80', bypass='<local>;10.*;*.example.com'),
         additions=dict(HTTP_PROXY='http://proxy.example:80', HTTPS_PROXY='http://proxy.example:80', NO_PROXY='localhost,127.0.0.1,[::1],*.example.com'), warning=True),
    case('explicit-no-proxy', dict(enabled=True, server='proxy.example:80', bypass='<local>'), env=dict(NO_PROXY='unchanged.local'),
         additions=dict(HTTP_PROXY='http://proxy.example:80', HTTPS_PROXY='http://proxy.example:80')),
    case('no-proxy-only', env=dict(NO_PROXY='unchanged.local'), source='direct'),
    case('long-bypass-warning', dict(enabled=True, server='proxy.example:80', bypass='a'*2049),
         additions=dict(HTTP_PROXY='http://proxy.example:80', HTTPS_PROXY='http://proxy.example:80'), warning=True),
    case('pac-guidance', dict(pac='https://pac.example/proxy.pac'), success=False),
    case('empty-manual', dict(enabled=True), success=False),
    case('socks-only-guidance', dict(enabled=True, server='socks=127.0.0.1:1080'), success=False),
    case('duplicate-protocol', dict(enabled=True, server='https=one:80;https=two:80'), success=False),
    case('duplicate-common', dict(enabled=True, server='one:80;two:80'), success=False),
    case('unsupported-scheme', dict(enabled=True, server='socks5://127.0.0.1:1080'), success=False),
    case('missing-host', dict(enabled=True, server='http://'), success=False),
    case('credential-guidance-redacted', dict(enabled=True, server='http://private-user:private-password@proxy.example:80'), success=False),
    case('registry-command-injection', dict(enabled=True, server='proxy:80" & echo unsafe>injected & rem "'), success=False),
    case('registry-delayed-injection', dict(enabled=True, server='proxy!unsafe!:80'), success=False),
    case('registry-percent-injection', dict(enabled=True, server='proxy%PATH%:80'), success=False),
]
for key in ('HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY'):
    CASES.append(case('explicit-'+key, dict(enabled=True, server='system.example:80', pac='https://pac.example'),
                      env={key:'http://private-user:private-password@explicit.example:18799', 'NO_PROXY':'keep.local'}, source='explicit'))


def check_cmd(folder, toolbin):
    """Invoke the exact helper in CMD with a native reg.exe query boundary."""
    import json
    import os
    from pathlib import Path
    import subprocess
    from test_windows_bat_entrypoints import cmd
    root = Path(__file__).resolve().parents[1]
    driver = folder / 'proxy-policy.cmd'
    driver.write_text('@echo off\nsetlocal DisableDelayedExpansion\ncall "'+str(root/'scripts/windows-build-proxy.bat')+'"\n'
                      'if errorlevel 1 exit /b 1\n"'+str(toolbin/'reg.exe')+'" --record-env\n', encoding='utf-8')
    for item in CASES:
        environment = {k:v for k,v in os.environ.items() if k.upper() not in ('HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','NO_PROXY')}
        system=item['input']['system']
        output=folder/'proxy-record.json'
        if output.exists():output.unlink()
        environment.update(item['input']['env'])
        environment.update(PATH=str(toolbin)+os.pathsep+environment['PATH'], FIXTURE_PROXY_RECORD=str(output),
            FIXTURE_PROXY_ENABLE='1' if system.get('enabled') else '0', FIXTURE_PROXY_SERVER=system.get('server',''),
            FIXTURE_PROXY_BYPASS=system.get('bypass',''), FIXTURE_PROXY_PAC=system.get('pac',''))
        result=subprocess.run(cmd('call "'+str(driver)+'"'), cwd=folder, env=environment, input='',
                              capture_output=True, text=True, timeout=10)
        assert (result.returncode==0)==item['success'],(item['name'],result.returncode,result.stdout,result.stderr)
        assert not (folder/'injected').exists(), item['name']
        assert 'private-user' not in result.stdout+result.stderr and 'private-password' not in result.stdout+result.stderr
        if item['success']:
            expected=dict(item['input']['env'],**item['expected']['additions'])
            actual={k:v for k,v in json.loads(output.read_text()).items() if v}
            assert actual==expected,(item['name'],actual,expected,result.stdout,result.stderr)
        else:
            assert 'HTTP_PROXY' in result.stdout and 'HTTPS_PROXY' in result.stdout,(item['name'],result.stdout)
        print('PASS actual CMD build proxy '+item['name'])
