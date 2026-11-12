#!/usr/bin/env python3
"""Embedded native helpers must retain their real dependency notices."""
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('windows_builder',ROOT/'scripts/build-windows-launcher.py')
build=importlib.util.module_from_spec(spec);spec.loader.exec_module(build)

class HelperNotices(unittest.TestCase):
 def test_empty_vendor_directory_does_not_use_aide_license(self):
  with tempfile.TemporaryDirectory() as temporary:
   root=Path(temporary);(root/'LICENSE').write_text('AIDE ONLY')
   dep=root/'vendor/example.invalid/dependency';dep.mkdir(parents=True)
   raw=b'Copyright \xa9 2017 VMware\r\nBSD-2 original license\r\n'
   (dep/'LICENSE_BSD-2.txt').write_bytes(raw);(dep/'NOTICE.txt').write_text('Original upstream notice')
   (dep/'child').mkdir();(dep/'child/LICENSE_list').write_text('Third party subcomponent notice')
   notices='\n'.join(build.helper_license_notices('example.invalid/dependency|\n',root))
   self.assertNotIn('AIDE ONLY',notices)
   for required in ('BSD-2 original license','Original upstream notice','Third party subcomponent notice',hashlib.sha256(raw).hexdigest(),'Windows-1252'):
    self.assertIn(required,notices)
   self.assertEqual((dep/'LICENSE_BSD-2.txt').read_bytes(),raw)
 def test_missing_vendor_notice_fails_instead_of_substituting_main(self):
  with tempfile.TemporaryDirectory() as temporary:
   root=Path(temporary);(root/'LICENSE').write_text('AIDE ONLY')
   dep=root/'vendor/example.invalid/missing';dep.mkdir(parents=True)
   with self.assertRaisesRegex(SystemExit,'Missing embedded helper dependency license'):
    build.helper_license_notices('example.invalid/missing|\n',root)
   (dep/'NOTICE.txt').write_text('A notice is not a complete license')
   with self.assertRaisesRegex(SystemExit,'Missing embedded helper dependency license'):
    build.helper_license_notices('example.invalid/missing|\n',root)
 def test_main_module_does_not_scan_runtime_or_private_state(self):
  with tempfile.TemporaryDirectory() as temporary:
   root=Path(temporary);(root/'LICENSE').write_text('AIDE MAIN')
   state=root/'.agent-state';state.mkdir();(state/'LICENSE-private').write_text('PRIVATE MUST NOT EMBED')
   notices='\n'.join(build.helper_license_notices('aide|'+str(root)+'\n',root))
   self.assertIn('AIDE MAIN',notices);self.assertNotIn('PRIVATE MUST NOT EMBED',notices)

if __name__=='__main__':unittest.main()
