#!/usr/bin/env python3
"""Portable production URL guards and SDK supply-chain rejection, no Windows claim."""
from pathlib import Path
import importlib.util
import shutil
import struct
import subprocess
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('native_build',ROOT/'scripts/build-windows-launcher.py')
build=importlib.util.module_from_spec(spec);spec.loader.exec_module(build)
class DesktopTests(unittest.TestCase):
 def test_real_production_policy(self):
  with tempfile.TemporaryDirectory() as directory:
   output=Path(directory)/'policy'
   subprocess.run([shutil.which('c++'),'-std=c++17',str(ROOT/'scripts/windows_desktop_policy.cpp'),'-o',str(output)],check=True)
   subprocess.run([str(output)],check=True)
 def test_multisize_aide_icon(self):
  data=(ROOT/'native/windows-launcher/resources/aide.ico').read_bytes()
  self.assertEqual(struct.unpack_from('<HH',data),(0,1))
  count=struct.unpack_from('<H',data,4)[0];sizes=[]
  for i in range(count):
   width,height,colors,reserved,planes,bits,length,offset=struct.unpack_from('<BBBBHHII',data,6+i*16)
   size=width or 256;self.assertEqual(size,height or 256);self.assertEqual((planes,bits),(1,32));frame=data[offset:offset+length]
   self.assertEqual(frame[:8],b'\x89PNG\r\n\x1a\n');self.assertEqual(struct.unpack_from('>II',frame,16),(size,size));sizes.append(size)
  self.assertEqual(sizes,[16,20,24,32,40,48,64,96,128,256])
 def test_sdk_cache_tampering_refuses_extraction(self):
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory);(root/('webview2-'+build.WEBVIEW2_VERSION+'.nupkg')).write_bytes(b'untrusted')
   with self.assertRaisesRegex(SystemExit,'SHA256 mismatch'):build.webview_sdk(root,'amd64')
   self.assertFalse((root/'webview2').exists())
if __name__=='__main__':unittest.main()
