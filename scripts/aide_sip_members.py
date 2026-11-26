"""Bridge a handset call to an approved Aide Messages voice invitation."""
import asyncio
import audioop
import contextlib
from fractions import Fraction
import secrets
import struct
import time
import uuid
from urllib.error import HTTPError

from aiortc import (AudioStreamTrack, RTCConfiguration, RTCIceServer,
                    RTCPeerConnection, RTCSessionDescription)
from aiortc.sdp import candidate_from_sdp
from av import AudioFrame, AudioResampler


class HandsetTrack(AudioStreamTrack):
    """WebRTC pulls PCM decoded by the existing G.711 RTP receiver."""
    def __init__(self, media):
        super().__init__()
        self.media = media
        self.samples = 0

    async def recv(self):
        pcm = await self.media.queue.get()
        frame = AudioFrame(format='s16', layout='mono', samples=len(pcm)//2)
        frame.planes[0].update(pcm)
        frame.sample_rate = 8000
        frame.time_base = Fraction(1, 8000)
        frame.pts = self.samples
        self.samples += frame.samples
        return frame


async def play_to_handset(track, media, codec):
    converter = AudioResampler(format='s16', layout='mono', rate=8000, frame_size=160)
    seq, timestamp, ssrc = secrets.randbits(16), secrets.randbits(32), secrets.randbits(32)
    while True:
        for frame in converter.resample(await track.recv()):
            pcm = bytes(frame.planes[0])[:frame.samples*2]
            payload = audioop.lin2ulaw(pcm, 2) if codec == 0 else audioop.lin2alaw(pcm, 2)
            packet = struct.pack('!BBHII', 128, codec, seq, timestamp, ssrc)+payload
            media.transport.sendto(packet, media.remote)
            seq, timestamp = (seq+1)&65535, (timestamp+frame.samples)&0xffffffff
            media.g.metrics['rtp_out'] += 1


async def member_call(gateway, host, incoming, media, rtp_port):
    headers, _, addr = incoming
    async def api(path, body=None):
        return await asyncio.to_thread(host.request, '/messages/'+path, body)

    try:
        reply = await api('dial', {'number':gateway.destination})
    except HTTPError as error:
        if error.code == 404:
            gateway.reject(incoming, 404, 'Not Found')
        raise
    inv = reply['invitation']
    owner = uuid.uuid4().hex
    permission = {'owner':owner, 'peer':inv['peer'], 'call':inv['resource'], 'invitation':inv['id']}
    state_path = 'dial-state?id='+inv['id']
    pc = None
    tasks = []
    try:
        if gateway.ended.is_set():
            return
        gateway.transport.sendto(gateway.response(headers, 180, 'Ringing'), addr)
        gateway.phase = 'ringing-member'
        deadline = time.monotonic()+gateway.args.ring_seconds
        while not gateway.ended.is_set() and time.monotonic() < deadline:
            await api('refresh', {'id':inv['id']})
            state = await api(state_path)
            if state['status'] == 'accepted':
                break
            if state['status'] != 'pending':
                if state['status'] == 'declined':
                    gateway.reject(incoming, 603, 'Decline')
                return
            await asyncio.sleep(.7)
        else:
            return
        await api('voice-lease', permission)
        servers = [RTCIceServer(**item) for item in await api('ice')]
        pc = RTCPeerConnection(RTCConfiguration(iceServers=servers))
        pc.addTrack(HandsetTrack(media))
        connected = asyncio.Event()
        finished = asyncio.Event()

        @pc.on('connectionstatechange')
        def connection_changed():
            if pc.connectionState == 'connected':
                connected.set()
            elif pc.connectionState in ('failed', 'closed'):
                finished.set()

        async def send_description(description):
            await pc.setLocalDescription(description)
            await api('signal', {**permission, 'data':{'description':{
                'type':pc.localDescription.type, 'sdp':pc.localDescription.sdp}}})

        async def negotiate():
            # Matches the existing browser's fingerprint-based offer election.
            if reply['self'] < inv['peer']:
                await send_description(await pc.createOffer())
            seen, pending = set(), []
            while not connected.is_set():
                state = await api(state_path)
                if state['status'] != 'accepted':
                    return
                for signal in state.get('signals') or ():
                    if signal['id'] in seen:
                        continue
                    seen.add(signal['id'])
                    data = signal['data']
                    if 'description' in data:
                        desc = RTCSessionDescription(**data['description'])
                        await pc.setRemoteDescription(desc)
                        for candidate in pending:
                            await pc.addIceCandidate(candidate)
                        pending.clear()
                        if desc.type == 'offer':
                            await send_description(await pc.createAnswer())
                    elif data.get('candidate', {}).get('candidate'):
                        item = data['candidate']
                        candidate = candidate_from_sdp(item['candidate'].removeprefix('candidate:'))
                        candidate.sdpMid = item.get('sdpMid')
                        candidate.sdpMLineIndex = item.get('sdpMLineIndex')
                        if pc.remoteDescription:
                            await pc.addIceCandidate(candidate)
                        else:
                            pending.append(candidate)
                await asyncio.sleep(.25)
            if gateway.ended.is_set():
                return
            # Recheck permission immediately before answering or releasing audio.
            await api('voice-lease', permission)
            remote_track = next(rx.track for rx in pc.getReceivers() if rx.track and rx.track.kind == 'audio')
            media.remote, codec = gateway.answer(incoming, rtp_port)
            gateway.event('sip_connected')
            await play_to_handset(remote_track, media, codec)

        async def keep_permission():
            while True:
                await asyncio.sleep(2)
                await asyncio.wait_for(api('voice-lease', permission), 5)
                if not connected.is_set() and time.monotonic() >= deadline:
                    return

        tasks = [asyncio.create_task(coro) for coro in (negotiate(), keep_permission(),
                 gateway.ended.wait(), finished.wait(), asyncio.sleep(gateway.args.call_seconds))]
        done, _ = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
        for task in done:
            task.result()
    finally:
        media.remote = None
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        if pc:
            await pc.close()
        # End the physical dialog before waiting for remote cleanup.
        await gateway.hangup()
        with contextlib.suppress(OSError, ValueError):
            await api('cancel', {'id':inv['id']})
        with contextlib.suppress(OSError, ValueError):
            await api('voice-release', permission)
