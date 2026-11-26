import json
from pathlib import Path
import tempfile
import unittest

from aide_sip_service import configure, parameters


class ServiceSettings(unittest.TestCase):
    def test_reconfigure_preserves_custom_ports_and_rejects_bad_target_atomically(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder)/'.aide-data/sip-phone/config.json'
            configure(folder, {'phone':'192.168.1.11','phone_user':'desk','port':5090,
                               'phone_port':5062,'aide_port':9998})
            configure(folder, {'phone':'192.168.1.12','port':None})
            original = path.read_bytes()
            self.assertEqual(json.loads(original), {'enabled':True,'phone':'192.168.1.12',
                'phone_user':'desk','port':5090,'phone_port':5062,'aide_port':9998})
            with self.assertRaises(ValueError):
                configure(folder, {'phone':'8.8.8.8'})
            self.assertEqual(path.read_bytes(), original)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_legacy_host_configuration_defaults(self):
        args = parameters({'phone':'10.0.0.8','enabled':True})
        self.assertEqual(dict(zip(args[::2],args[1::2])),{'--phone':'10.0.0.8',
            '--phone-user':'1001','--assistant':'auto','--sip-port':'5060',
            '--phone-port':'5060','--aide-port':'9999'})
