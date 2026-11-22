import io
import json
import wave
import argparse
import asyncio
import audioop
import math
import struct
import unittest

from aide_sip_phone import Gateway, Media, TelephoneDownsampler, decode_rtp, media_offer, playback_gain, wait_phone_or_callback


class Transport:
    def __init__(self):
        self.sent = []

    def sendto(self, data, addr):
        self.sent.append((data, addr))


class PhoneProtocolTests(unittest.IsolatedAsyncioTestCase):
    def test_only_assistant_extension_can_open_a_call(self):
        gateway = Gateway(argparse.Namespace(phone='10.0.0.2', report=None), '10.0.0.1')
        gateway.transport = Transport()
        gateway.datagram_received(b'INVITE sip:1234@10.0.0.1 SIP/2.0\r\nCall-ID: other\r\n\r\n', ('10.0.0.2',5060))
        self.assertTrue(gateway.transport.sent[-1][0].startswith(b'SIP/2.0 404'))
        self.assertTrue(gateway.incoming.empty())
        gateway.datagram_received(b'INVITE sip:1000@10.0.0.1 SIP/2.0\r\nCall-ID: assistant\r\n\r\n', ('10.0.0.2',5060))
        self.assertEqual(gateway.incoming.qsize(),1)

    def test_playback_gain_boosts_quiet_voice_without_clipping(self):
        quiet = struct.pack('<h', 2000)*160
        loud = struct.pack('<hh', 30000, -30000)*80
        self.assertEqual(audioop.rms(playback_gain(quiet, 2), 2), 4000)
        self.assertLessEqual(audioop.max(playback_gain(loud, 2), 2), 28001)
        self.assertEqual(playback_gain(b'\0'*320, 2), b'\0'*320)

    def test_resampler_rejects_aliases_and_preserves_chunks(self):
        def sine(hz):
            return b''.join(struct.pack('<h', round(10000*math.sin(2*math.pi*hz*n/24000))) for n in range(2400))
        low, high = sine(1000), sine(6000)
        converted = TelephoneDownsampler().convert(low)
        self.assertGreater(audioop.rms(converted[128:], 2), 6800)
        self.assertLess(audioop.rms(TelephoneDownsampler().convert(high)[128:], 2), 100)
        streaming = TelephoneDownsampler()
        chunks = b''.join(streaming.convert(low[n:n+158]) for n in range(0, len(low), 158))
        self.assertEqual(chunks, converted)

    def test_rtp_extensions_and_padding(self):
        packet = struct.pack('!BBHII', 0xb1, 8, 42, 0, 1)
        packet += b'CSRC'+struct.pack('!HH', 0, 1)+b'EXT!'+b'voice'+b'\0\x02'
        self.assertEqual(decode_rtp(packet), (8, 42, b'voice'))
        for invalid in [b'', packet[:12], packet[:-1]+b'\xff']:
            with self.assertRaises(ValueError):
                decode_rtp(invalid)

    def test_sdp_only_selected_phone_and_g711(self):
        body = 'c=IN IP4 10.0.0.2\r\nm=audio 4000 RTP/AVP 8 101\r\n'
        self.assertEqual(media_offer(body, '10.0.0.2'), (('10.0.0.2', 4000), 8))
        for invalid in [body.replace('4000', '0'), body.replace('8 101', '101'), body.replace('10.0.0.2', '10.0.0.3')]:
            with self.assertRaises(ValueError):
                media_offer(invalid, '10.0.0.2')

    def test_unknown_peer_cannot_register_or_supply_media(self):
        gateway = Gateway(argparse.Namespace(phone='10.0.0.2', report=None), '10.0.0.1')
        gateway.transport = Transport()
        register = b'REGISTER sip:10.0.0.1 SIP/2.0\r\nCall-ID: fixture\r\nCSeq: 1 REGISTER\r\n\r\n'
        gateway.datagram_received(register, ('10.0.0.3', 5060))
        self.assertEqual(gateway.metrics['registrations'], 0)
        self.assertEqual(gateway.transport.sent, [])
        gateway.datagram_received(register, ('10.0.0.2', 5060))
        self.assertEqual(gateway.metrics['registrations'], 1)
        self.assertTrue(gateway.transport.sent[0][0].startswith(b'SIP/2.0 200'))
        media = Media(gateway)
        media.remote = ('10.0.0.2', 4000)
        packet = struct.pack('!BBHII', 128, 0, 1, 0, 1)+b'\xff'*160
        media.datagram_received(packet, ('10.0.0.3', 4000))
        media.datagram_received(packet, ('10.0.0.2', 4001))
        self.assertEqual(media.queue.qsize(), 0)
        media.datagram_received(packet, media.remote)
        media.datagram_received(packet, media.remote)
        self.assertEqual(media.queue.qsize(), 1)
        self.assertEqual(len(media.queue.get_nowait()), 320)


class CallbackProtocolTests(unittest.IsolatedAsyncioTestCase):
    def gateway(self):
        g = Gateway(argparse.Namespace(phone='10.0.0.2', phone_user='1001', sip_port=5060, ring_seconds=.2, report=None), '10.0.0.1')
        g.transport = Transport()
        return g

    async def test_idle_poll_claims_without_opening_model(self):
        gateway = self.gateway()
        class Host:
            def request(self, path, body):
                assert path.endswith('/claim')
                return {'callback': {'id': 'new-job', 'assistantId': 'assistant'}}
        incoming, callback = await wait_phone_or_callback(gateway, Host(), True)
        self.assertIsNone(incoming)
        self.assertEqual(callback['id'], 'new-job')
        self.assertTrue(gateway.active)
        gateway.datagram_received(b'INVITE sip:1000@10.0.0.1 SIP/2.0\r\nCall-ID: competing\r\n\r\n', ('10.0.0.2', 5060))
        self.assertTrue(gateway.transport.sent[-1][0].startswith(b'SIP/2.0 486'))
        gateway.transport.sent.clear()
        self.assertFalse(gateway.transport.sent)

    async def test_incoming_during_claim_releases_only_reservation(self):
        gateway = self.gateway()
        loop = asyncio.get_running_loop()
        released = []
        arrived = asyncio.Event()
        class Host:
            def request(self, path, body):
                if path.endswith('/release'):
                    released.append(body)
                    return {'released': True}
                def incoming():
                    gateway.active = True
                    gateway.incoming.put_nowait(('real incoming', '', ('10.0.0.2', 5060)))
                    arrived.set()
                loop.call_soon_threadsafe(incoming)
                # Wait until the event loop has consumed the incoming INVITE.
                import time
                while not arrived.is_set(): time.sleep(.001)
                return {'callback': {'id': 'reserved'}}
        incoming, callback = await wait_phone_or_callback(gateway, Host(), True)
        self.assertIsNone(callback)
        self.assertEqual(incoming[0], 'real incoming')
        self.assertEqual(released, [{'id': 'reserved'}])

    async def test_callback_dial_answers_with_ack_and_expected_codec(self):
        gateway = self.gateway()
        dial = asyncio.create_task(gateway.dial(42000))
        await asyncio.sleep(0)
        body = 'c=IN IP4 10.0.0.2\r\nm=audio 4000 RTP/AVP 0\r\n'
        packet = ('SIP/2.0 200 OK\r\nCall-ID: '+gateway.call_id+'\r\nTo: <sip:1001@10.0.0.2>;tag=phone\r\nContact: <sip:1001@10.0.0.2>\r\n\r\n'+body).encode()
        gateway.datagram_received(packet, ('10.0.0.2', 5060))
        self.assertEqual(await dial, (('10.0.0.2', 4000), 0))
        self.assertTrue(gateway.transport.sent[0][0].startswith(b'INVITE '))
        self.assertTrue(gateway.transport.sent[-1][0].startswith(b'ACK '))

    async def test_callback_rejected_or_unanswered_never_redials(self):
        for rejection in (False, True):
            gateway = self.gateway()
            dial = asyncio.create_task(gateway.dial(42000))
            await asyncio.sleep(0)
            if rejection:
                packet = ('SIP/2.0 486 Busy Here\r\nCall-ID: '+gateway.call_id+'\r\nTo: <sip:1001@10.0.0.2>;tag=phone\r\n\r\n').encode()
                gateway.datagram_received(packet, ('10.0.0.2', 5060))
            with self.assertRaises((asyncio.TimeoutError, RuntimeError)):
                await dial
            invites = [packet for packet,_ in gateway.transport.sent if packet.startswith(b'INVITE ')]
            self.assertEqual(len(invites), 1)
            self.assertTrue(gateway.transport.sent[-1][0].startswith(b'CANCEL '))


if __name__ == '__main__':
    unittest.main()


class PhoneSpeechTests(unittest.TestCase):
    def test_chinese_speech_decoder_preserves_pcm_and_rejects_wrong_format(self):
        from aide_sip_phone import synthesize_phone_speech
        class Opener:
            def open(self, request, timeout):
                self.body = json.loads(request.data)
                return io.BytesIO(self.wav)
        host = argparse.Namespace(base='https://localhost:9999', token='fixture', opener=Opener())
        def wav(channels):
            result = io.BytesIO()
            with wave.open(result, 'wb') as output:
                output.setnchannels(channels);output.setsampwidth(2);output.setframerate(24000)
                output.writeframes(b'\1\0'*2400*channels)
            return result.getvalue()
        host.opener.wav = wav(1)
        self.assertEqual(len(synthesize_phone_speech(host, '接着聊天津的天气吗？')), 4800)
        self.assertEqual(host.opener.body['language'], 'Chinese')
        host.opener.wav = wav(2)
        with self.assertRaises(ValueError): synthesize_phone_speech(host, '不能播放错误格式')


class CloseDiagnosticTests(unittest.TestCase):
    def test_close_diagnostic_reports_codes_without_private_reason(self):
        from aide_sip_phone import safe_close_diagnostic
        error = argparse.Namespace(rcvd=argparse.Namespace(code=1011, reason='private URL and key'), sent=argparse.Namespace(code=1011, reason='keepalive ping timeout'))
        self.assertEqual(safe_close_diagnostic(error), {'rcvd_code':1011, 'sent_code':1011, 'close_reason':'websocket-ping-timeout'})


class UserHangupPlaybackTests(unittest.IsolatedAsyncioTestCase):
    def test_farewell_drains_matching_response_and_handles_late_asr(self):
        from aide_sip_phone import CallEndPlayback
        end = CallEndPlayback()
        end.observe({'type':'aide.call_end','reason':'user-request','turnId':'last','responseId':'bye'}, 10)
        end.observe({'type':'response.done','response':{'id':'old'}}, 10.2)
        self.assertFalse(end.ready(11, 0))
        end.observe({'type':'response.done','response':{'id':'bye','status':'completed'}}, 11)
        self.assertFalse(end.ready(11, 320))
        self.assertFalse(end.ready(11.1, 0))
        self.assertTrue(end.ready(11.4, 0))
        late = CallEndPlayback()
        late.observe({'type':'response.done','response':{'id':'bye','status':'completed'}}, 1)
        late.observe({'type':'aide.call_end','reason':'user-request','turnId':'last','responseId':'bye'}, 2)
        self.assertFalse(late.ready(2.1, 0))
        self.assertTrue(late.ready(2.4, 0))

    def test_no_response_times_out_duplicate_does_not_extend_and_new_turn_revokes(self):
        from aide_sip_phone import CallEndPlayback
        end = CallEndPlayback()
        request = {'type':'aide.call_end','reason':'user-request','turnId':'last'}
        end.observe(request, 10)
        end.observe(request, 15)
        self.assertFalse(end.ready(15.9, 0))
        self.assertTrue(end.ready(16, 320))
        end.observe({'type':'aide.call_end_cancel','turnId':'last'}, 16)
        self.assertFalse(end.ready(100, 0))

    async def test_host_hangup_returns_through_real_sip_bye_teardown(self):
        from aide_sip_phone import CallEndPlayback, caller_requested_hangup
        gateway = Gateway(argparse.Namespace(phone='10.0.0.2', phone_user='1001', sip_port=5060, report=None), '10.0.0.1')
        gateway.transport = Transport()
        gateway.call_id = 'fixture-call'
        gateway.cseq = 1
        gateway.make_request = lambda method, **kwargs: (method+' sip:1001@10.0.0.2 SIP/2.0\r\n').encode()
        end = CallEndPlayback()
        end.observe({'type':'aide.call_end','reason':'user-request','turnId':'last','responseDone':True}, asyncio.get_running_loop().time()-.4)
        end.ready(asyncio.get_running_loop().time()-.4, 0)
        await asyncio.wait_for(caller_requested_hangup(end, bytearray(), gateway), .3)
        gateway.ended.set()  # Provider/client socket closes before SIP teardown.
        await gateway.responses.put(('SIP/2.0 200 OK', {'cseq':['2 BYE']}, '', ('10.0.0.2',5060)))
        await asyncio.wait_for(gateway.hangup(), .3)
        self.assertEqual(len(gateway.transport.sent), 1)
        self.assertTrue(gateway.transport.sent[0][0].startswith(b'BYE '))
        self.assertTrue(gateway.metrics['user_requested_hangup'])
        await gateway.hangup()
        self.assertEqual(len(gateway.transport.sent), 1, 'duplicate SIP BYE')
        gateway.peer_ended = True
        gateway.bye_sent = False
        await gateway.hangup()
        self.assertEqual(len(gateway.transport.sent), 1, 'must not re-hang-up peer-ended call')
