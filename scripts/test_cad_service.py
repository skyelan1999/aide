"""CAD isolation protocol: auth, upload limits, chunking, busy and cancellation."""
import http.client
import importlib.util
import json
import os
from pathlib import Path
import shutil
import tempfile
import threading
import time
import unittest
ROOT=Path(__file__).resolve().parents[1]

class CadServiceTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='aide-cad-test-');self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        shutil.copy(ROOT/'scripts/cad/server.py',self.root/'server.py')
        (self.root/'convert.py').write_text('''import json,sys,time,os
from pathlib import Path
source=Path(sys.argv[1]).read_bytes()
print(json.dumps({'kind':'progress','size':len(source)}),flush=True)
if source==b'slow':
 Path(os.environ['CAD_TEST_PID']).write_text(str(os.getpid()))
 time.sleep(30)
print(json.dumps({'kind':'complete','triangles':12}),flush=True)
''')
        token=self.root/'token';token.write_text('fixture-secret')
        spec=importlib.util.spec_from_file_location('cad_fixture',self.root/'server.py');self.module=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.module);self.module.TOKEN_FILE=str(token)
        self.server=self.module.ThreadingHTTPServer(('127.0.0.1',0),self.module.Handler);self.server.daemon_threads=True
        threading.Thread(target=self.server.serve_forever,daemon=True).start()
        self.addCleanup(self.server.server_close);self.addCleanup(self.server.shutdown)
    def connection(self):
        c=http.client.HTTPConnection('127.0.0.1',self.server.server_port,timeout=5);self.addCleanup(c.close);return c
    def test_auth_and_body_limits(self):
        c=self.connection();c.request('POST','/convert',body=b'STEP');r=c.getresponse();self.assertEqual(r.status,401);r.read()
        c=self.connection();c.request('POST','/convert',headers={'Authorization':'Bearer fixture-secret','Content-Length':str((384<<20)+1)});r=c.getresponse();self.assertEqual(r.status,400);r.read()
    def test_chunked_upload_and_complete(self):
        c=self.connection();c.request('POST','/convert',body=iter([b'STEP',b'-test']),headers={'Authorization':'Bearer fixture-secret'},encode_chunked=True);r=c.getresponse();self.assertEqual(r.status,200)
        lines=[json.loads(s) for s in r.read().splitlines()];self.assertEqual(lines[0]['size'],9);self.assertEqual(lines[-1]['kind'],'complete')
    def test_close_kills_child_and_releases_gate(self):
        pidfile=self.root/'pid';old=os.environ.get('CAD_TEST_PID');os.environ['CAD_TEST_PID']=str(pidfile)
        self.addCleanup(lambda:os.environ.pop('CAD_TEST_PID',None) if old is None else os.environ.__setitem__('CAD_TEST_PID',old))
        c=self.connection();c.request('POST','/convert',body=b'slow',headers={'Authorization':'Bearer fixture-secret'});r=c.getresponse();self.assertEqual(r.status,200);r.readline()
        deadline=time.monotonic()+3
        while not pidfile.exists() and time.monotonic()<deadline:time.sleep(.02)
        pid=int(pidfile.read_text())
        other=self.connection();other.request('POST','/convert',body=b'STEP',headers={'Authorization':'Bearer fixture-secret'});response=other.getresponse();self.assertEqual(response.status,429);response.read()
        r.close();c.close()
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            try:os.kill(pid,0)
            except ProcessLookupError:break
            time.sleep(.05)
        else:self.fail('conversion child survived client cancellation')
        # The child can be reaped just before the handler closes its pipe and
        # releases the semaphore. Wait on that actual cleanup boundary rather
        # than treating process exit and parent cleanup as one atomic event.
        acquired=self.module.GATE.acquire(timeout=1)
        self.assertTrue(acquired,'CAD handler did not release its gate after child cancellation')
        self.module.GATE.release()
if __name__=='__main__':unittest.main()
