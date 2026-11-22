#!/usr/bin/env python3
"""Single-phone LAN SIP/RTP gateway to Aide's existing assistant call relay.

Requires Python 3.9-3.12 (audioop) and websockets==15.0.1. No audio is saved.
Only the explicitly selected IPv4 peer can signal or send media. Aide TLS and
identity gates remain in force; its token is read from disk, never sent to SIP.
--normal selects ordinary workbench calls; the default is a read-only harness.
This is a local single-phone gateway, not a general PBX or Internet SIP service.
"""
import io
import wave
import argparse
from array import array
import asyncio
import audioop
import base64
import contextlib
import ipaddress
import json
import math
import os
from pathlib import Path
from datetime import datetime, timezone
import re
import secrets
import signal
import socket
import ssl
import struct
import sys
import time
import urllib.request
import uuid


def parse_sip(data):
    if len(data) > 65507:
        raise ValueError('oversize SIP')
    head, _, body = data.decode('utf-8', errors='replace').partition('\r\n\r\n')
    lines = head.split('\r\n')
    headers = {}
    for line in lines[1:]:
        key, sep, value = line.partition(':')
        if sep:
            headers.setdefault(key.lower(), []).append(value.strip())
    return lines[0], headers, body


def header(headers, name):
    return headers.get(name, [''])[0]


def media_offer(body, peer):
    connection = re.search(r'^c=IN IP4 ([^\r\n ]+)', body, re.M)
    audio = re.search(r'^m=audio (\d+) RTP/AVP ([\d ]+)', body, re.M)
    if not connection or connection[1] != peer or not audio:
        raise ValueError('SDP must use the selected phone address')
    port = int(audio[1])
    types = [int(n) for n in audio[2].split()]
    if not 1024 <= port <= 65535:
        raise ValueError('invalid RTP port')
    codec = next((n for n in types if n in (0, 8)), None)
    if codec is None:
        raise ValueError('phone must offer PCMU or PCMA')
    return (peer, port), codec


def decode_rtp(packet):
    if len(packet) < 12 or packet[0] >> 6 != 2:
        raise ValueError('invalid RTP header')
    offset = 12 + 4 * (packet[0] & 15)
    if packet[0] & 16:
        if len(packet) < offset + 4:
            raise ValueError('invalid RTP extension')
        offset += 4 + struct.unpack('!H', packet[offset+2:offset+4])[0] * 4
    end = len(packet)
    if packet[0] & 32:
        padding = packet[-1]
        if not padding or padding > end - offset:
            raise ValueError('invalid RTP padding')
        end -= padding
    if offset >= end:
        raise ValueError('empty RTP payload')
    return packet[1] & 127, struct.unpack('!H', packet[2:4])[0], packet[offset:end]


class TelephoneDownsampler:
    """Streaming 24-to-8 kHz FIR, with anti-alias filtering before decimation.

    audioop.ratecv alone is interpolation, not a telephone-band low-pass.
    Preserve filter history and decimation phase across arbitrary WS chunks.
    """
    def __init__(self):
        size, cutoff = 63, 3400/24000
        taps = []
        for i in range(size):
            x = i-(size-1)/2
            sinc = 2*cutoff if x == 0 else math.sin(2*math.pi*cutoff*x)/(math.pi*x)
            taps.append(sinc*(.5-.5*math.cos(2*math.pi*i/(size-1))))
        total = sum(taps)
        self.taps = [x/total for x in taps]
        self.history, self.phase = [0]*(size-1), 0

    def convert(self, pcm):
        values = array('h')
        values.frombytes(pcm)
        # Aide PCM and SIP transcoding are explicitly little endian.
        if sys.byteorder != 'little':
            values.byteswap()
        data = self.history+list(values)
        offset, result = len(self.history), array('h')
        for n in range(self.phase, len(values), 3):
            value = round(sum(tap*data[offset+n-i] for i, tap in enumerate(self.taps)))
            result.append(max(-32768, min(32767, value)))
        self.phase = (self.phase-len(values)) % 3
        self.history = data[-len(self.history):]
        if sys.byteorder != 'little':
            result.byteswap()
        return result.tobytes()


def playback_gain(pcm, gain):
    """Bound each 20 ms frame's peak before G.711 companding, never clip."""
    peak = audioop.max(pcm, 2)
    return audioop.mul(pcm, 2, min(gain, 28000/peak)) if peak else pcm


class Gateway(asyncio.DatagramProtocol):
    def __init__(self, args, local_ip):
        self.args, self.ip = args, local_ip
        self.transport = None
        self.responses = asyncio.Queue(maxsize=128)
        self.incoming = asyncio.Queue(maxsize=1)
        self.ended = asyncio.Event()
        self.peer_ended = False
        self.bye_sent = False
        self.call_id = ''
        self.active = False
        self.last_answer = None
        self.last_ack = None
        self.invitation = None
        self.metrics = {'registrations': 0, 'signalling': [], 'rtp_in': 0,
                        'rtp_out': 0, 'input_energy_packets': 0,
                        'transcriptions': 0, 'assistant_audio_bytes': 0,
                        'output_rms_max': 0, 'output_peak': 0, 'rtp_send_late_ms': 0,
                        'assistant_responses': 0, 'read_only': False}
        self.metrics['native_tool_calls'] = []
        self.metrics['approval_query_receipts'] = []
        self.started = time.monotonic()
        self.phase = 'waiting'

    def event(self, name, **values):
        if name == 'sip_connected': self.phase = 'connected'
        elif name == 'gateway_stopped': self.phase = 'stopped'
        elif name == 'sip_dialing': self.phase = 'dialing'
        row = dict(event=name, seconds=round(time.monotonic()-self.started, 3), **values)
        print(json.dumps(row, ensure_ascii=False), flush=True)
        if name.startswith('sip_'):
            self.metrics['signalling'].append(row)
        self.save()
        if name == 'gateway_stopped' and self.active and self.args.report:
            p = Path(self.args.report)
            archive = p.with_name(p.stem+'-'+self.call_id.split('@')[0][:24]+p.suffix)
            archive.write_text(json.dumps(self.metrics, ensure_ascii=False, indent=2)+'\n')
            os.chmod(archive, 0o600)

    def save(self):
        if self.args.report:
            p = Path(self.args.report)
            if not self.active and p.exists():
                return
            tmp = p.with_suffix('.tmp')
            tmp.write_text(json.dumps(self.metrics, ensure_ascii=False, indent=2)+'\n')
            os.chmod(tmp, 0o600)
            tmp.replace(p)

    def connection_made(self, transport):
        self.transport = transport

    def response(self, headers, code, phrase, body='', extras=()):
        lines = ['SIP/2.0 %d %s' % (code, phrase)]
        for value in headers.get('via', []):
            lines.append('Via: '+value)
        to = header(headers, 'to')
        if ';tag=' not in to:
            to += ';tag=aide-local'
        lines += ['From: '+header(headers, 'from'), 'To: '+to,
                  'Call-ID: '+header(headers, 'call-id'), 'CSeq: '+header(headers, 'cseq')]
        lines.extend(extras)
        if body:
            lines.append('Content-Type: application/sdp')
        return ('\r\n'.join(lines+['Content-Length: '+str(len(body.encode())), '', body])).encode()

    def datagram_received(self, data, addr):
        if addr[0] != self.args.phone:
            return
        try:
            first, headers, body = parse_sip(data)
        except ValueError:
            return
        if first.startswith('SIP/2.0 '):
            if self.last_ack and header(headers, 'call-id') == self.call_id and first.startswith('SIP/2.0 200') and header(headers, 'cseq').endswith(' INVITE'):
                self.transport.sendto(self.last_ack, addr)
                return
            if header(headers, 'call-id') == self.call_id and not self.responses.full():
                self.responses.put_nowait((first, headers, body, addr))
            return
        method = first.split(' ')[0]
        cid = header(headers, 'call-id')
        if method == 'REGISTER':
            # This isolated test registrar accepts only the selected phone IP.
            contact = header(headers, 'contact')
            extra = ['Contact: '+contact, 'Expires: 300'] if contact else ['Expires: 300']
            self.transport.sendto(self.response(headers, 200, 'OK', extras=extra), addr)
            self.metrics['registrations'] += 1
            self.event('sip_registered')
        elif method == 'OPTIONS':
            self.transport.sendto(self.response(headers, 200, 'OK'), addr)
        elif method == 'INVITE':
            if not first.split()[1].lower().startswith('sip:1000@'):
                self.transport.sendto(self.response(headers, 404, 'Not Found'), addr)
                return
            if cid == self.call_id:
                self.transport.sendto(self.last_answer or self.response(headers, 100, 'Trying'), addr)
            elif self.active or self.incoming.full():
                self.transport.sendto(self.response(headers, 486, 'Busy Here'), addr)
            else:
                self.active, self.call_id = True, cid
                self.invitation = headers
                self.transport.sendto(self.response(headers, 100, 'Trying'), addr)
                self.incoming.put_nowait((headers, body, addr))
        elif method in ('BYE', 'CANCEL') and cid == self.call_id:
            self.peer_ended = True
            self.transport.sendto(self.response(headers, 200, 'OK'), addr)
            if method == 'CANCEL' and self.invitation and not self.last_answer:
                self.transport.sendto(self.response(self.invitation, 487, 'Request Terminated'), addr)
            self.event('sip_'+method.lower())
            self.ended.set()

    def sdp(self, port):
        return '\r\n'.join(['v=0', 'o=aide 1 1 IN IP4 '+self.ip,
                            's=Aide read-only phone', 'c=IN IP4 '+self.ip, 't=0 0',
                            'm=audio %d RTP/AVP 0 8' % port,
                            'a=rtpmap:0 PCMU/8000', 'a=rtpmap:8 PCMA/8000',
                            'a=sendrecv', 'a=ptime:20', ''])

    async def dial(self, rtp_port):
        uri = 'sip:%s@%s' % (self.args.phone_user, self.args.phone)
        self.call_id = uuid.uuid4().hex+'@'+self.ip
        self.active = True
        branch = 'z9hG4bK'+uuid.uuid4().hex
        self.from_header = '"XiaoMian" <sip:1000@%s>;tag=%s' % (self.ip, secrets.token_hex(8))
        self.to_header = '<'+uri+'>'
        self.target = uri
        self.cseq = 1

        def request(method, body='', new_branch=False):
            via = 'z9hG4bK'+uuid.uuid4().hex if new_branch else branch
            lines = [method+' '+self.target+' SIP/2.0',
                     'Via: SIP/2.0/UDP %s:%d;branch=%s;rport' % (self.ip, self.args.sip_port, via),
                     'Max-Forwards: 70', 'From: '+self.from_header, 'To: '+self.to_header,
                     'Call-ID: '+self.call_id, 'CSeq: %d %s' % (self.cseq, method),
                     'Contact: <sip:1000@%s:%d>' % (self.ip, self.args.sip_port)]
            if body:
                lines.append('Content-Type: application/sdp')
            return ('\r\n'.join(lines+['Content-Length: '+str(len(body.encode())), '', body])).encode()

        self.make_request = request
        peer = (self.args.phone, 5060)
        invite = request('INVITE', self.sdp(rtp_port))
        self.transport.sendto(invite, peer)
        try:
            async def wait_answer():
                provisional = False
                while True:
                    try:
                        first, headers, body, _ = await asyncio.wait_for(self.responses.get(), 1)
                    except asyncio.TimeoutError:
                        if not provisional:
                            self.transport.sendto(invite, peer)
                        continue
                    code = int(first.split()[1])
                    self.event('sip_response', status=code)
                    provisional = code < 200
                    if code < 200:
                        continue
                    self.to_header = header(headers, 'to')
                    if code != 200:
                        self.transport.sendto(request('ACK'), peer)
                        raise RuntimeError('phone rejected call: '+str(code))
                    contact = re.search(r'<([^>]+)>', header(headers, 'contact'))
                    if contact:
                        self.target = contact[1]
                    self.last_ack = request('ACK', new_branch=True)
                    self.transport.sendto(self.last_ack, peer)
                    return media_offer(body, self.args.phone)
            return await asyncio.wait_for(wait_answer(), self.args.ring_seconds)
        except BaseException:
            self.transport.sendto(request('CANCEL'), peer)
            raise

    def answer(self, incoming, rtp_port):
        headers, body, addr = incoming
        remote, codec = media_offer(body, self.args.phone)
        if self.ended.is_set():
            raise RuntimeError('phone cancelled before assistant became ready')
        self.from_header = header(headers, 'to')
        if ';tag=' not in self.from_header:
            self.from_header += ';tag=aide-local'
        self.to_header = header(headers, 'from')
        contact = re.search(r'<([^>]+)>', header(headers, 'contact'))
        self.target = contact[1] if contact else 'sip:%s@%s' % (self.args.phone_user, self.args.phone)
        self.cseq = 1
        def request(method, new_branch=False):
            lines = [method+' '+self.target+' SIP/2.0',
                     'Via: SIP/2.0/UDP %s:%d;branch=z9hG4bK%s;rport' % (self.ip, self.args.sip_port, uuid.uuid4().hex),
                     'Max-Forwards: 70', 'From: '+self.from_header, 'To: '+self.to_header,
                     'Call-ID: '+self.call_id, 'CSeq: %d %s' % (self.cseq, method), 'Content-Length: 0', '', '']
            return '\r\n'.join(lines).encode()
        self.make_request = request
        sdp = self.sdp(rtp_port).replace('RTP/AVP 0 8', 'RTP/AVP '+str(codec))
        other = 8 if codec == 0 else 0
        sdp = re.sub(r'a=rtpmap:%d [^\r\n]+\r\n' % other, '', sdp)
        self.last_answer = self.response(headers, 200, 'OK', sdp, ['Contact: <sip:1000@%s:%d>' % (self.ip, self.args.sip_port)])
        self.transport.sendto(self.last_answer, addr)
        return remote, codec

    async def hangup(self):
        if hasattr(self, 'make_request') and not self.peer_ended and not self.bye_sent:
            self.bye_sent = True
            self.cseq += 1
            self.transport.sendto(self.make_request('BYE', new_branch=True), (self.args.phone, 5060))
            with contextlib.suppress(asyncio.TimeoutError):
                while True:
                    first, headers, _, _ = await asyncio.wait_for(self.responses.get(), 2)
                    if header(headers, 'cseq').endswith(' BYE') and first.startswith('SIP/2.0 200'):
                        self.event('sip_hangup_ok')
                        break


class Media(asyncio.DatagramProtocol):
    def __init__(self, gateway):
        self.g = gateway
        self.queue = asyncio.Queue(maxsize=100)
        self.remote = None
        self.transport = None
        self.seen = set()

    def connection_made(self, transport):
        self.transport = transport

    def datagram_received(self, data, addr):
        if not self.remote or addr != self.remote:
            return
        try:
            payload_type, sequence, payload = decode_rtp(data)
        except ValueError:
            return
        if payload_type not in (0, 8) or len(payload) > 1600 or sequence in self.seen:
            return
        self.seen.add(sequence)
        if len(self.seen) > 2048:
            self.seen.clear()
        pcm = audioop.ulaw2lin(payload, 2) if payload_type == 0 else audioop.alaw2lin(payload, 2)
        self.g.metrics['rtp_in'] += 1
        rms = audioop.rms(pcm, 2)
        self.g.metrics['input_rms_max'] = max(self.g.metrics.get('input_rms_max', 0), rms)
        if rms > 200:
            self.g.metrics['input_energy_packets'] += 1
        if self.queue.full():
            self.queue.get_nowait()
        self.queue.put_nowait(pcm)


class SIPHostClient:
    """Authenticated local host control; never sent to the physical phone."""
    def __init__(self, data, tls, token):
        self.token, self.base = token, 'https://localhost:9999'
        directory = data/'sip'; directory.mkdir(mode=0o700, exist_ok=True)
        path = directory/'host-key'
        try:
            fd = os.open(path, os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600)
        except FileExistsError:
            pass
        else:
            with os.fdopen(fd, 'w') as out: out.write(secrets.token_hex(32)+'\n')
        if path.is_symlink() or not path.is_file() or path.stat().st_size > 128:
            raise ValueError('Invalid SIP host key file')
        self.key = path.read_text().strip()
        if not re.fullmatch('[a-f0-9]{64}', self.key): raise ValueError('Invalid SIP host key')
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=tls))

    def request(self, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(self.base+'/api'+path, data=data,
            headers={'Authorization':'Bearer '+self.token, 'X-Aide-SIP-Key':self.key,
                     'Origin':self.base, 'Content-Type':'application/json'})
        with self.opener.open(req, timeout=15 if path == '/assistant-sip/opening' else 10) as response: return json.load(response)


async def wait_phone_or_callback(gateway, host, callbacks):
    loop = asyncio.get_running_loop()
    claim_nonce = uuid.uuid4().hex
    while True:
        try:
            return await asyncio.wait_for(gateway.incoming.get(), 2), None
        except asyncio.TimeoutError:
            if not callbacks or gateway.active: continue
        try:
            reply = await loop.run_in_executor(None, host.request, '/assistant-sip/callbacks/claim', {'requestId':claim_nonce})
            job = reply.get('callback')
            if not job:
                claim_nonce=uuid.uuid4().hex
                continue
            # A physical incoming INVITE can arrive while HTTP is in flight.
            if gateway.active or not gateway.incoming.empty():
                await loop.run_in_executor(None, host.request, '/assistant-sip/callbacks/release', {'id':job['id']})
                return await gateway.incoming.get(), None
            # Reserve the single line synchronously before preflight HTTP. A new
            # INVITE arriving after this point receives busy instead of being
            # overwritten by the outbound call's Call-ID.
            gateway.active = True
            return None, job
        except (OSError, ValueError):
            # Idle failures do not start paid model connections or repeat dials.
            await asyncio.sleep(2)


class CallEndPlayback:
    """Host-owned hangup: drain this turn's farewell with a six-second cap."""
    def __init__(self):
        self.cancel()
        self.completed = set()

    def cancel(self):
        self.turn = None
        self.response = None
        self.requested = None
        self.done = False
        self.drained_at = None

    def observe(self, event, now):
        kind = event.get('type')
        if kind == 'aide.call_end_cancel':
            self.cancel()
        elif kind == 'aide.call_end' and event.get('reason') == 'user-request' and event.get('turnId'):
            if self.turn == event['turnId']: return
            self.turn, self.requested = event['turnId'], now
            self.drained_at = None
            self.response = event.get('responseId')
            self.done = bool(event.get('responseDone')) or self.response in self.completed
        elif kind == 'response.created' and self.requested is not None and not self.response:
            self.response = event.get('response', {}).get('id')
        elif kind == 'response.done':
            response = event.get('response', {})
            rid = response.get('id')
            if rid and response.get('status', 'completed') == 'completed':
                self.completed.add(rid)
                if self.requested is not None and rid == self.response: self.done = True

    def ready(self, now, buffered_bytes):
        if self.requested is None: return False
        elapsed = now-self.requested
        if not self.done or buffered_bytes:
            self.drained_at = None
        elif self.drained_at is None:
            self.drained_at = now
        return elapsed >= 6 or self.drained_at is not None and now-self.drained_at >= .25


async def caller_requested_hangup(playback, output, gateway):
    loop = asyncio.get_running_loop()
    while True:
        if playback.ready(loop.time(), len(output)):
            gateway.metrics['user_requested_hangup'] = True
            gateway.event('assistant_hangup_requested')
            # Returning ends the bridge through its existing teardown, which
            # closes the paid socket and sends SIP BYE. A WebSocket EOF is not
            # evidence that the physical phone has already hung up.
            return
        await asyncio.sleep(.05)


def safe_close_diagnostic(error):
    values = {}
    for name in ('rcvd', 'sent'):
        close = getattr(error, name, None)
        code = getattr(close, 'code', None)
        if isinstance(code, int): values[name+'_code'] = code
        if getattr(close, 'reason', None) == 'keepalive ping timeout':
            values['close_reason'] = 'websocket-ping-timeout'
    return values


def synthesize_phone_speech(host, text):
    req = urllib.request.Request(host.base+'/api/tts/synthesize', data=json.dumps({'text':text,'language':'Chinese'}).encode(),
        headers={'Authorization':'Bearer '+host.token,'Origin':host.base,'Content-Type':'application/json'})
    with host.opener.open(req, timeout=30) as response:
        raw = response.read(4*1024*1024+1)
    if len(raw)>4*1024*1024: raise ValueError('phone audio too large')
    with wave.open(io.BytesIO(raw)) as wav:
        if wav.getsampwidth()!=2 or wav.getnchannels()!=1: raise ValueError('phone requires mono PCM WAV')
        rate=wav.getframerate();pcm=wav.readframes(wav.getnframes())
    if not pcm or len(pcm)/2/rate>45: raise ValueError('invalid phone speech duration')
    if rate!=24000: pcm,_=audioop.ratecv(pcm,2,1,rate,24000,None)
    return pcm


async def bridge(args):
    from websockets.asyncio.client import connect
    data = Path(args.data).resolve()
    token = (data/'auth/access-token').read_text().strip()
    tls = ssl.create_default_context(cafile=str(data/'certs/cert.pem'))
    # Aide's local certificate is a non-CA leaf. Trust that exact provisioned
    # leaf as the chain anchor while retaining signature/hostname validation.
    # Python 3.9's LibreSSL supports this flag but doesn't expose its constant.
    tls.verify_flags |= getattr(ssl, 'VERIFY_X509_PARTIAL_CHAIN', 0x80000)
    host = SIPHostClient(data, tls, token)
    sid = args.assistant
    base = 'https://localhost:9999'
    probe = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    probe.connect((args.phone, 5060))
    ip = probe.getsockname()[0]
    probe.close()
    loop = asyncio.get_running_loop()
    gateway = Gateway(args, ip)
    sip_transport, _ = await loop.create_datagram_endpoint(lambda: gateway, local_addr=(ip, args.sip_port))
    media = Media(gateway)
    rtp_transport, _ = await loop.create_datagram_endpoint(lambda: media, local_addr=(ip, 0))
    gateway.event('gateway_ready', local_ip=ip, sip_port=args.sip_port)
    tasks = []
    status_path = data/'sip'/'host-status.json'
    status_path.parent.mkdir(mode=0o700, exist_ok=True)
    def publish_status():
        value = {'server':ip, 'port':args.sip_port, 'phone':args.phone,
                 'account':args.phone_user, 'extension':'1000',
                 'mode':'normal' if args.normal else 'read-only', 'state':gateway.phase,
                 'updatedAt':datetime.now(timezone.utc).isoformat()}
        tmp = status_path.with_suffix('.tmp')
        tmp.write_text(json.dumps(value)+'\n');os.chmod(tmp,0o600);tmp.replace(status_path)
    async def status_loop():
        while True:
            publish_status()
            await asyncio.sleep(5)
    status_task = asyncio.create_task(status_loop())
    incoming, callback, call_ticket = None, None, None
    callback_outcome = 'failed'
    try:
        if not args.dial:
            gateway.event('waiting_for_phone', extension='1000')
            incoming, callback = await wait_phone_or_callback(gateway, host, args.normal and args.serve)
        if callback: sid = callback['assistantId']
        def preflight():
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=tls))
            nonlocal sid
            if sid == 'auto':
                listing = urllib.request.Request(base+'/api/sessions', headers={'Authorization': 'Bearer '+token})
                with opener.open(listing, timeout=10) as response:
                    sessions = json.load(response)
                sid = next((s['id'] for s in sessions if s.get('kind') == 'assistant'), '')
                if not sid: raise RuntimeError('Open and unlock the assistant first')
            call_request = urllib.request.Request(base+'/api/sessions/'+sid+'/assistant-call', headers={'Authorization': 'Bearer '+token})
            with opener.open(call_request, timeout=10) as response:
                return json.load(response)
        metadata = await loop.run_in_executor(None, preflight)
        if args.normal:
            call_ticket = await loop.run_in_executor(None, host.request, '/assistant-sip/calls',
                {'action':'open','assistantId':sid,'phone':args.phone,'account':args.phone_user,
                 'callbackId':callback['id'] if callback else ''})
        port = rtp_transport.get_extra_info('sockname')[1]
        callback_media = None
        if callback:
            gateway.event('sip_dialing', callback_id=callback['id'])
            publish_status()
            try:
                callback_media = await gateway.dial(port)
            except (asyncio.TimeoutError, RuntimeError):
                callback_outcome = 'unanswered'
                raise
        if callback:
            report = await loop.run_in_executor(None, host.request, '/assistant-sip/callbacks/report', {'callId':call_ticket['callId']})
            pcm = await loop.run_in_executor(None, synthesize_phone_speech, host, report['text'])
            media.remote, codec = callback_media
            gateway.metrics.update(callback_id=callback['id'],callback_reported=False,read_only=True,assistant_audio_bytes=len(pcm))
            gateway.event('sip_connected', codec='PCMU' if codec==0 else 'PCMA', report_only=True)
            samples = TelephoneDownsampler().convert(pcm)
            seq, timestamp, ssrc = secrets.randbits(16), secrets.randbits(32), secrets.randbits(32)
            deadline=loop.time();last_check=0
            for offset in range(0,len(samples),320):
                if gateway.ended.is_set(): return
                if loop.time()-last_check>2:
                    check=await loop.run_in_executor(None,host.request,'/assistant-sip/calls',{'action':'check','callId':call_ticket['callId']})
                    if not check.get('valid'): return
                    last_check=loop.time();deadline=max(deadline,last_check)
                frame=playback_gain(samples[offset:offset+320].ljust(320,b'\0'),args.output_gain)
                payload=audioop.lin2ulaw(frame,2) if codec==0 else audioop.lin2alaw(frame,2)
                rtp_transport.sendto(struct.pack('!BBHII',128,codec,seq,timestamp,ssrc)+payload,media.remote)
                gateway.metrics['rtp_out']+=1;seq=(seq+1)&65535;timestamp=(timestamp+160)&0xffffffff
                deadline+=.02;await asyncio.sleep(max(0,deadline-loop.time()))
            await asyncio.sleep(.2)
            if gateway.ended.is_set(): return
            await loop.run_in_executor(None,host.request,'/assistant-sip/calls',{'action':'close','callId':call_ticket['callId'],'outcome':'reported'})
            callback_outcome='reported';gateway.metrics['callback_reported']=True
            gateway.event('callback_report_sent',callback_id=callback['id'],session_number=report['sessionNumber'])
            return
        opening_pcm = b''
        if args.normal:
            opening = await loop.run_in_executor(None, host.request, '/assistant-sip/opening', {'callId':call_ticket['callId']})
            if opening.get('text'):
                opening_pcm = await loop.run_in_executor(None, synthesize_phone_speech, host, opening['text'])
        if gateway.ended.is_set(): return
        url = 'wss://localhost:9999/api/sessions/'+sid+'/assistant-call/socket'
        if not args.normal: url += '?readOnly=1'
        headers = {'Authorization':'Bearer '+token}
        if call_ticket: headers.update({'X-Aide-SIP-Key':host.key,'X-Aide-SIP-Call':call_ticket['callId']})
        async with connect(url, ssl=tls, origin=base, proxy=None,
                           subprotocols=['aide-realtime'], additional_headers=headers,
                           max_size=2**20, close_timeout=3) as ws:
            ready = json.loads(await asyncio.wait_for(ws.recv(), 20))
            if ready.get('type') != 'aide.ready' or ready.get('readOnly') is not (not args.normal):
                raise RuntimeError('Aide did not confirm the selected call scope')
            if bool(ready.get('sipCallback')) != bool(callback): raise RuntimeError('Unexpected callback scope')
            gateway.metrics['callback_id'] = callback['id'] if callback else None
            gateway.metrics['callback_reported'] = False
            gateway.metrics['read_only'] = not args.normal
            gateway.event('assistant_ready', model=ready.get('model'), input_rate=metadata['inputRate'])
            port = rtp_transport.get_extra_info('sockname')[1]
            media.remote, codec = callback_media if callback else (await gateway.dial(port) if args.dial else gateway.answer(incoming, port))
            gateway.event('sip_connected', codec='PCMU' if codec == 0 else 'PCMA')
            output = bytearray(TelephoneDownsampler().convert(opening_pcm))
            if opening_pcm:
                gateway.metrics['assistant_audio_bytes'] += len(opening_pcm)
                gateway.event('history_opening_ready', audio_bytes=len(opening_pcm))
            response_ids = set()
            callback_done = asyncio.Event()
            callback_interrupted = False
            call_end = CallEndPlayback()

            async def microphone():
                state = None
                while True:
                    pcm = await media.queue.get()
                    pcm, state = audioop.ratecv(pcm, 2, 1, 8000, metadata['inputRate'], state)
                    await ws.send(json.dumps({'type': 'input_audio_buffer.append', 'audio': base64.b64encode(pcm).decode()}))

            async def upstream():
                nonlocal callback_interrupted
                downsample = TelephoneDownsampler()
                async for raw in ws:
                    event = json.loads(raw)
                    kind = event.get('type')
                    call_end.observe(event, loop.time())
                    if kind == 'aide.call_end':
                        gateway.event('host_hangup_request')
                    if kind == 'aide.error':
                        gateway.event('assistant_error', message=event.get('message', 'relay error'))
                        raise RuntimeError('Aide realtime relay reported an error')
                    if kind == 'response.audio.delta':
                        pcm = base64.b64decode(event['delta'], validate=True)
                        if len(pcm) % 2:
                            raise ValueError('invalid provider PCM')
                        gateway.metrics['assistant_audio_bytes'] += len(pcm)
                        gateway.metrics['output_rms_max'] = max(gateway.metrics['output_rms_max'], audioop.rms(pcm, 2))
                        gateway.metrics['output_peak'] = max(gateway.metrics['output_peak'], audioop.max(pcm, 2))
                        pcm = await loop.run_in_executor(None, downsample.convert, pcm)
                        if len(output) + len(pcm) > 320000:
                            raise RuntimeError('audio buffer exceeded 20 seconds')
                        output.extend(pcm)
                    elif kind == 'input_audio_buffer.speech_started':
                        if callback and not gateway.metrics['callback_reported'] and gateway.metrics['assistant_audio_bytes']:
                            callback_interrupted = True
                        output.clear()
                        downsample = TelephoneDownsampler()
                        gateway.event('phone_speech_started')
                    elif kind == 'conversation.item.input_audio_transcription.completed':
                        gateway.metrics['transcriptions'] += 1
                        # No transcript/audio saved; optional ephemeral console for acceptance.
                        values = {'text': event.get('transcript')} if args.show_transcripts else {}
                        gateway.event('phone_transcription', **values)
                    elif kind == 'response.audio_transcript.done':
                        values = {'text': event.get('transcript')} if args.show_transcripts else {}
                        gateway.event('assistant_transcription', **values)
                    elif kind == 'response.output_item.added':
                        item = event.get('item', {})
                        if item.get('type') == 'function_call':
                            receipt = {'name': item.get('name'), 'call_id': item.get('call_id'), 'response_id': event.get('response_id')}
                            gateway.metrics['native_tool_calls'].append(receipt)
                            gateway.event('native_tool_call', tool_name=receipt['name'], call_id=receipt['call_id'], response_id=receipt['response_id'])
                    elif kind == 'aide.approval':
                        # Host event, never a model's spoken assertion. Do not
                        # copy proposal contents, commands or file paths.
                        receipt = {key: event[key] for key in ('sessionNumber', 'kind', 'round', 'expiresAt') if key in event}
                        receipt['offer_present'] = 'sessionNumber' in event
                        gateway.metrics['approval_query_receipts'].append(receipt)
                        gateway.event('approval_query_receipt', **receipt)
                    elif kind == 'aide.tool_result':
                        receipt = {key: event[key] for key in ('tool', 'operation', 'callId', 'readOnly', 'status', 'sessionNumber', 'pendingCount') if key in event}
                        gateway.metrics['approval_query_receipts'].append(receipt)
                        gateway.event('host_tool_result', **receipt)
                    elif kind == 'aide.speech_review':
                        gateway.event('speech_review', reason=event.get('reason'), audio_bytes=event.get('audioBytes'))
                    elif kind == 'response.done':
                        rid = event.get('response', {}).get('id')
                        if rid and rid not in response_ids:
                            response_ids.add(rid)
                            gateway.metrics['assistant_responses'] += 1
                            gateway.event('assistant_response_done')
                            if callback and event.get('response', {}).get('status', 'completed') == 'completed' and gateway.metrics['assistant_audio_bytes']:
                                callback_done.set()
                gateway.ended.set()

            async def speaker():
                sequence, timestamp, ssrc = secrets.randbits(16), secrets.randbits(32), secrets.randbits(32)
                deadline = loop.time()
                while True:
                    pcm = bytes(output[:320])
                    del output[:320]
                    pcm += b'\0' * (320-len(pcm))
                    pcm = playback_gain(pcm, args.output_gain)
                    payload = audioop.lin2ulaw(pcm, 2) if codec == 0 else audioop.lin2alaw(pcm, 2)
                    packet = struct.pack('!BBHII', 128, codec, sequence, timestamp, ssrc)+payload
                    rtp_transport.sendto(packet, media.remote)
                    gateway.metrics['rtp_out'] += 1
                    sequence, timestamp = (sequence+1) & 65535, (timestamp+160) & 0xffffffff
                    deadline += .02
                    await asyncio.sleep(max(0, deadline-loop.time()))
                    gateway.metrics['rtp_send_late_ms'] = max(gateway.metrics['rtp_send_late_ms'], round(max(0, loop.time()-deadline)*1000, 2))
                    if loop.time()-deadline > .1:
                        deadline = loop.time()

            async def heartbeat():
                while True:
                    await ws.send(json.dumps({'type': 'aide.ping'}))
                    gateway.save()
                    await asyncio.sleep(10)

            async def callback_receipt():
                nonlocal callback_outcome
                if not callback:
                    await gateway.ended.wait()
                    return
                await callback_done.wait()
                while output and not gateway.ended.is_set(): await asyncio.sleep(.05)
                if not gateway.ended.is_set() and not callback_interrupted:
                    await loop.run_in_executor(None, host.request, '/assistant-sip/calls',
                        {'action':'close','callId':call_ticket['callId'],'outcome':'reported'})
                    callback_outcome = 'reported'
                    gateway.metrics['callback_reported'] = True
                    gateway.event('callback_report_sent', callback_id=callback['id'])
                await gateway.ended.wait()

            tasks = [asyncio.create_task(coro()) for coro in (microphone, upstream, speaker, heartbeat, callback_receipt)]
            tasks.append(asyncio.create_task(caller_requested_hangup(call_end, output, gateway)))
            end = asyncio.create_task(gateway.ended.wait())
            timer = asyncio.create_task(asyncio.sleep(min(args.call_seconds, 180) if callback else args.call_seconds))
            tasks += [end, timer]
            done, _ = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
            for task in done:
                if not task.cancelled() and task.exception():
                    raise task.exception()
            await ws.send(json.dumps({'type': 'aide.hangup'}))
    finally:
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        if incoming and not gateway.last_answer and not gateway.ended.is_set():
            headers, _, addr = incoming
            sip_transport.sendto(gateway.response(headers, 480, 'Temporarily Unavailable'), addr)
        await gateway.hangup()
        if call_ticket:
            with contextlib.suppress(OSError, ValueError):
                await loop.run_in_executor(None, host.request, '/assistant-sip/calls',
                    {'action':'close','callId':call_ticket['callId'],'outcome':callback_outcome if callback else ''})
        elif callback:
            with contextlib.suppress(OSError, ValueError):
                await loop.run_in_executor(None, host.request, '/assistant-sip/callbacks/release', {'id':callback['id']})
        sip_transport.close()
        rtp_transport.close()
        gateway.event('gateway_stopped')
        status_task.cancel()
        await asyncio.gather(status_task, return_exceptions=True)
        publish_status()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phone', required=True)
    parser.add_argument('--phone-user', default='1001')
    parser.add_argument('--assistant', required=True)
    parser.add_argument('--data', default='.aide-data/storage/data')
    parser.add_argument('--sip-port', type=int, default=5060)
    parser.add_argument('--dial', action='store_true')
    parser.add_argument('--normal', action='store_true', help='use the same ordinary assistant call session as the workbench')
    parser.add_argument('--serve', action='store_true', help='wait for successive inbound calls; no paid provider connection while idle')
    parser.add_argument('--ring-seconds', type=int, default=60)
    parser.add_argument('--call-seconds', type=int, default=180)
    parser.add_argument('--report')
    parser.add_argument('--show-transcripts', action='store_true')
    parser.add_argument('--output-gain', type=float, default=2.0, help='telephone playback gain, peak-limited per 20ms frame')
    args = parser.parse_args()
    if not ipaddress.IPv4Address(args.phone).is_private or not re.fullmatch(r'[A-Za-z0-9_-]{1,32}', args.phone_user):
        parser.error('explicit private IPv4 phone and simple local SIP user required')
    if not 1024 <= args.sip_port <= 65535 or not 1 <= args.call_seconds <= 1800 or not 1 <= args.ring_seconds <= 120:
        parser.error('invalid local port or call duration')
    if not 1 <= args.output_gain <= 3:
        parser.error('output gain must be between 1 and 3')
    if args.serve and args.dial:
        parser.error('--serve waits for inbound calls and cannot be combined with --dial')
    async def run():
        task = asyncio.current_task()
        loop = asyncio.get_running_loop()
        for signum in (signal.SIGTERM, signal.SIGINT):
            loop.add_signal_handler(signum, task.cancel)
        while True:
            try:
                await bridge(args)
            except Exception as error:
                # No URLs, credentials or raw provider exception bodies in logs.
                print(json.dumps({'event': 'gateway_error', 'kind': type(error).__name__, **safe_close_diagnostic(error)}), flush=True)
                if not args.serve:
                    raise SystemExit(1)
                await asyncio.sleep(2)
            if not args.serve:
                return
            # Let UDP transport close callbacks release the listening socket.
            await asyncio.sleep(.1)
    try:
        asyncio.run(run())
    except asyncio.CancelledError:
        pass


if __name__ == '__main__':
    main()
