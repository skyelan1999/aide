"""One Unix PTY; stdin/stdout are private newline-delimited JSON, never a shell pipe."""
import base64
import fcntl
import json
import os
import pty
import select
import signal
import struct
import sys
import termios
import time


def emit(**fields):
    sys.stdout.write(json.dumps(fields, separators=(',', ':')) + '\n')
    sys.stdout.flush()


def main():
    config = json.loads(sys.stdin.buffer.readline(131073))
    key_fd = None
    if config.get('identityMaterial'):
        # Keep key bytes in a private Linux memfd owned by this PTY supervisor.
        # OpenSSH closes inherited fds, so address the supervisor's live fd.
        material = base64.b64decode(config.pop('identityMaterial'), validate=True)
        if not material or len(material) > 65536 or not hasattr(os, 'memfd_create'):
            raise ValueError('Memory-backed SSH keys require Linux')
        key_fd = os.memfd_create('aide-terminal-identity', os.MFD_CLOEXEC)
        os.fchmod(key_fd, 0o600)
        os.write(key_fd, material)
        os.lseek(key_fd, 0, os.SEEK_SET)
        config['argv'] = [f'/proc/{os.getpid()}/fd/{key_fd}' if arg == '@AIDE_MEMORY_KEY@' else arg for arg in config['argv']]
        del material
    pid, master = pty.fork()
    if pid == 0:
        os.chdir(config['cwd'])
        os.environ['TERM'] = 'xterm-256color'
        os.environ['COLORTERM'] = 'truecolor'
        os.environ.pop('AIDE_PLUGIN_SETTINGS', None)
        os.environ.pop('AIDE_DAEMON', None)
        os.execvpe(config['argv'][0], config['argv'], os.environ)
    def resize(cols, rows):
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))
    resize(config['cols'], config['rows'])
    emit(type='ready', pid=pid)
    incoming = b''
    outgoing = bytearray()
    finished = False
    def stop(*_):
        nonlocal finished
        finished = True
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    status = None
    try:
        os.set_blocking(master, False)
        while not finished:
            readable, writable, _ = select.select([master, sys.stdin.buffer], [master] if outgoing else [], [], .2)
            if sys.stdin.buffer in readable:
                block = os.read(sys.stdin.fileno(), 65536)
                if not block:
                    break  # daemon died: never leave the session detached from its owner
                incoming += block
                if len(incoming) > 131072:
                    raise ValueError('PTY control frame too large')
                while b'\n' in incoming:
                    line, incoming = incoming.split(b'\n', 1)
                    msg = json.loads(line)
                    if msg['type'] == 'input':
                        outgoing.extend(base64.b64decode(msg['data'], validate=True))
                        if len(outgoing) > 262144:
                            raise ValueError('PTY input queue full')
                    elif msg['type'] == 'resize':
                        resize(msg['cols'], msg['rows'])
                    elif msg['type'] == 'close':
                        finished = True
            if master in writable and outgoing:
                try:
                    sent = os.write(master, outgoing)
                    del outgoing[:sent]
                except BlockingIOError:
                    pass
            if master in readable:
                try:
                    block = os.read(master, 8192)
                except OSError:
                    break
                if not block:
                    break
                emit(type='output', data=base64.b64encode(block).decode('ascii'))
            found, child_status = os.waitpid(pid, os.WNOHANG)
            if found:
                status = child_status
                # Drain final bytes before publishing exit (e.g. a short shell command).
                while True:
                    try:
                        block = os.read(master, 8192)
                        if not block:
                            break
                        emit(type='output', data=base64.b64encode(block).decode('ascii'))
                    except OSError:
                        break
                break
    finally:
        # The foreground process group can differ from the original interactive shell.
        try:
            fg = os.tcgetpgrp(master)
            if fg > 0 and fg != pid:
                os.killpg(fg, signal.SIGHUP)
        except ProcessLookupError:
            pass
        except OSError:
            pass
        os.close(master)
        if key_fd is not None:
            os.close(key_fd)
        for sig in (signal.SIGHUP, signal.SIGTERM, signal.SIGKILL):
            try:
                os.killpg(pid, sig)
            except ProcessLookupError:
                pass
            deadline = time.monotonic() + .3
            while status is None and time.monotonic() < deadline:
                try:
                    found, child_status = os.waitpid(pid, os.WNOHANG)
                except ChildProcessError:
                    break
                if found:
                    status = child_status
                    break
                time.sleep(.02)
            if status is not None:
                break
        emit(type='exit', code=os.waitstatus_to_exitcode(status) if status is not None else None)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        emit(type='error', message=str(error))
        sys.exit(1)
