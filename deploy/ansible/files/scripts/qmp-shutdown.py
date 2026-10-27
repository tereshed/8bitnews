#!/usr/bin/env python3
"""ExecStop helper for 8bitnews-hub-vm.service.

Sends a QMP system_powerdown to the running qemu process over its QMP
unix socket and waits for the socket to close (the guest OS has shut
down) before returning, so systemd only considers the unit stopped once
the VM has actually powered off rather than being killed mid-write.
"""
import json
import socket
import sys
import time

TIMEOUT_SECONDS = 90


def _recv_until(sock, predicate, deadline):
    buf = b""
    while time.monotonic() < deadline:
        sock.settimeout(max(deadline - time.monotonic(), 0.1))
        try:
            chunk = sock.recv(4096)
        except socket.timeout:
            continue
        if not chunk:
            return True  # peer closed the socket: qemu has exited
        buf += chunk
        while b"\n" in buf:
            line, buf = buf.split(b"\n", 1)
            if not line:
                continue
            try:
                msg = json.loads(line)
            except ValueError:
                continue
            if predicate(msg):
                return False
    return False


def main(argv):
    if len(argv) != 2:
        print("usage: qmp-shutdown.py <qmp-socket-path>", file=sys.stderr)
        return 2

    sock_path = argv[1]
    try:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.connect(sock_path)
    except OSError:
        # No socket: qemu isn't running, nothing to stop.
        return 0

    deadline = time.monotonic() + TIMEOUT_SECONDS
    with sock:
        _recv_until(sock, lambda m: "QMP" in m, deadline)
        sock.sendall(json.dumps({"execute": "qmp_capabilities"}).encode() + b"\n")
        _recv_until(sock, lambda m: m.get("return") == {}, deadline)
        sock.sendall(json.dumps({"execute": "system_powerdown"}).encode() + b"\n")
        closed = _recv_until(sock, lambda m: False, deadline)
        if not closed:
            # Guest didn't power off in time; force qemu to exit so the
            # unit doesn't hang forever.
            try:
                sock.sendall(json.dumps({"execute": "quit"}).encode() + b"\n")
            except OSError:
                pass
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
