"""Real WebRTC transport between a synthetic G.711 handset and a voice peer."""
import argparse
import asyncio
import audioop
import contextlib
import math
import struct
import unittest

from aide_sip_phone import Gateway, Media, decode_rtp
try:
    from aiortc import AudioStreamTrack, RTCConfiguration, RTCPeerConnection, RTCSessionDescription
    from aide_sip_members import member_call
except ImportError:
    AudioStreamTrack = None


class Capture:
    def __init__(self):
        self.sent = []

    def sendto(self, packet, addr):
        self.sent.append((packet, addr))


@unittest.skipUnless(AudioStreamTrack, 'install sip-phone-requirements.txt for WebRTC tests')
class MemberCalls(unittest.IsolatedAsyncioTestCase):
    async def exercise(self, handset_offers):
        class Tone(AudioStreamTrack):
            async def recv(self):
                frame = await super().recv()
                frame.planes[0].update(tone)
                return frame

        tone = b''.join(struct.pack('<h', int(5000*math.sin(i*.28))) for i in range(160))
        loop = asyncio.get_running_loop()
        pc = RTCPeerConnection(RTCConfiguration(iceServers=[]))
        pc.addTrack(Tone())
        inv = {'id':'1'*32, 'resource':'2'*32, 'peer':'b'*64}
        signals, clean = [], []
        accepted = False
        async def api(path, body):
            nonlocal accepted
            if path == '/messages/dial':
                self.assertEqual(body, {'number':'00123456'})
                return {'invitation':inv,'self':('a' if handset_offers else 'c')*64}
            if path == '/messages/refresh':
                accepted = True
                if not handset_offers and not pc.localDescription:
                    await pc.setLocalDescription(await pc.createOffer())
                    signals.append({'id':'offer','data':{'description':{'type':'offer','sdp':pc.localDescription.sdp}}})
            elif path.startswith('/messages/dial-state?'):
                return {'status':'accepted' if accepted else 'pending','signals':list(signals)}
            elif path == '/messages/ice':
                return []
            elif path == '/messages/signal':
                desc = body['data']['description']
                await pc.setRemoteDescription(RTCSessionDescription(**desc))
                if desc['type'] == 'offer':
                    await pc.setLocalDescription(await pc.createAnswer())
                    signals.append({'id':'answer','data':{'description':{'type':'answer','sdp':pc.localDescription.sdp}}})
            elif path == '/messages/voice-lease':
                self.assertTrue(accepted)
            elif path in ('/messages/cancel', '/messages/voice-release'):
                clean.append(path)
            else:
                self.fail(path)
            return {'ok':True}

        class Host:
            def request(self, path, body=None):
                return asyncio.run_coroutine_threadsafe(api(path, body), loop).result(timeout=5)

        args = argparse.Namespace(phone='10.0.0.2', phone_user='1001', normal=True,
                                  sip_port=5060, report=None, ring_seconds=12, call_seconds=20)
        g = Gateway(args, '10.0.0.1')
        g.transport = Capture()
        media = Media(g)
        media.transport = Capture()
        addr = (args.phone,5072)
        body = 'v=0\r\nc=IN IP4 10.0.0.2\r\nm=audio 4000 RTP/AVP 0 8\r\n'
        invite = ('INVITE sip:00123456@10.0.0.1 SIP/2.0\r\nCall-ID: test\r\nCSeq: 1 INVITE\r\n\r\n'+body).encode()
        g.datagram_received(invite, addr)
        incoming = await asyncio.wait_for(g.incoming.get(),1)
        task = asyncio.create_task(member_call(g, Host(), incoming, media, 5000))

        async def exchange():
            for i in range(400):
                if task.done():
                    task.result()
                    self.fail('call ended before audio')
                media.datagram_received(struct.pack('!BBHII',128,0,i,i*160,1)+audioop.lin2ulaw(tone,2),(args.phone,4000))
                if any(packet.startswith(b'SIP/2.0 200') for packet,_ in g.transport.sent) and len(media.transport.sent)>10:
                    break
                await asyncio.sleep(.02)
            self.assertTrue(any(packet.startswith(b'SIP/2.0 180') for packet,_ in g.transport.sent))
            self.assertTrue(media.transport.sent)
            self.assertTrue(any(audioop.rms(audioop.ulaw2lin(decode_rtp(packet)[2],2),2)>500 for packet,_ in media.transport.sent))
            remote = pc.getReceivers()[0].track
            for _ in range(20):
                media.datagram_received(struct.pack('!BBHII',128,0,i+1,(i+1)*160,1)+audioop.lin2ulaw(tone,2),(args.phone,4000))
                i+=1
                frame = await asyncio.wait_for(remote.recv(),1)
                if audioop.rms(bytes(frame.planes[0])[:frame.samples*2],2)>500:
                    break
            else:
                self.fail('no non-silent handset audio at the WebRTC peer')
            g.datagram_received(b'BYE sip:00123456@10.0.0.1 SIP/2.0\r\nCall-ID: test\r\nCSeq: 2 BYE\r\n\r\n',addr)
            await asyncio.wait_for(task,2)
            self.assertCountEqual(clean,['/messages/cancel','/messages/voice-release'])

        try:
            await asyncio.wait_for(exchange(),15)
        finally:
            if not task.done(): task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await task
            await pc.close()

    async def test_handset_offer(self):
        await self.exercise(True)

    async def test_remote_offer(self):
        await self.exercise(False)
