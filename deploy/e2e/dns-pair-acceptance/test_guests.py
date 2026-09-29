#!/usr/bin/env python3
"""Offline tests: guest identity refusal, probe streaming, tunnel argv, and the
probe's real UDP/TCP/AXFR path against a loopback DNS stub."""

from __future__ import annotations

import socket
import struct
import subprocess
import sys
import tempfile
import threading
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import guest_probe as gp  # noqa: E402
import guests  # noqa: E402
import topology as topo  # noqa: E402

fixture = guests.fixture
KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZpeHR1cmUta2V5LWZvci10ZXN0cy1vbmx5 test"
CATALOG = "catalog-c000020b.celikpanel.invalid"


def make_plan(root: Path, cell_id: str) -> dict:
    pins = {
        "debian13": fixture.ImagePin("debian13", "debian", "13", "amd64", "https://x/d.qcow2", "d.qcow2",
                                     "sha512", "0" * 128, 1),
        "arch": fixture.ImagePin("arch", "arch", "rolling", "amd64", "https://x/a.qcow2", "a.qcow2",
                                 "sha256", "0" * 64, 1),
    }
    return fixture.build_cell_plan(root, pins, cell_id, KEY)


class Runner:
    def __init__(self, plan: dict, *, wrong_uuid: bool = False) -> None:
        self.plan = plan
        self.wrong_uuid = wrong_uuid
        self.calls: list[tuple[list[str], bytes | None]] = []

    def __call__(self, argv, **kwargs):  # noqa: ANN001, ANN003
        self.calls.append((argv, kwargs.get("input")))
        command = argv[-1]
        if command == fixture.GUEST_IDENTITY_COMMAND:
            port = argv[argv.index("-p") + 1]
            node = next(name for name, value in self.plan["nodes"].items()
                        if str(value["management"]["ssh_port"]) == port)
            uuid = fixture.plan_vm_uuid(self.plan, node)
            if self.wrong_uuid:
                uuid = "11111111-1111-1111-1111-111111111111"
            text = (f"product_uuid={uuid}\nschema={fixture.PLAN_SCHEMA}\ncell_id={self.plan['cell_id']}\n"
                    f"node={node}\nboot_id=22222222-2222-2222-2222-222222222222\n")
            return subprocess.CompletedProcess(argv, 0, text.encode(), b"")
        return subprocess.CompletedProcess(argv, 0, b'{"command": "versions"}\n', b"")


class GuestsTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        root = Path(self.temporary.name).resolve() / "work"
        fixture.initialize_work_root(root)
        self.topology = topo.resolve("bind/bind", primary_node="arch")
        self.cell = self.topology.cell_id("t1")
        self.plan = make_plan(root, self.cell)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def test_topology_cell_ids_are_fixture_valid(self) -> None:
        for name, node in (("bind/bind", "arch"), ("bind/bind", "debian13"),
                           ("bind-primary/pdns-secondary", None), ("pdns-primary/bind-secondary", None)):
            cell = topo.resolve(name, primary_node=node).cell_id("run-01")
            self.assertEqual(fixture.validate_cell_id(cell), cell)

    def test_mutating_command_rechecks_identity_first(self) -> None:
        runner = Runner(self.plan)
        guest = guests.Guests(self.plan, Path("/nonexistent/id"), runner=runner)
        guest.run("arch", "sudo -n true", mutating=True)
        self.assertEqual(runner.calls[0][0][-1], fixture.GUEST_IDENTITY_COMMAND)
        self.assertEqual(runner.calls[1][0][-1], "sudo -n true")
        self.assertIn("-p", runner.calls[1][0])
        self.assertEqual(runner.calls[1][0][runner.calls[1][0].index("-p") + 1], "2202")

    def test_foreign_guest_is_refused_before_any_command(self) -> None:
        runner = Runner(self.plan, wrong_uuid=True)
        guest = guests.Guests(self.plan, Path("/nonexistent/id"), runner=runner)
        with self.assertRaisesRegex(fixture.FixtureError, "not the cell's own disposable guest"):
            guest.run("debian13", "sudo -n systemctl disable --now celikpanel-panel.service", mutating=True)
        self.assertEqual(len(runner.calls), 1)
        with self.assertRaises(guests.GuestError):
            guest.verify("freebsd")

    def test_probe_streams_the_script_read_only(self) -> None:
        runner = Runner(self.plan)
        guest = guests.Guests(self.plan, Path("/nonexistent/id"), runner=runner)
        value = guest.probe("arch", "native", "--engine", "bind", "--zone", "a b.example")
        self.assertEqual(value["command"], "versions")
        argv, stdin = runner.calls[-1]
        self.assertEqual(len(runner.calls), 1, "a read-only probe needs no identity round trip")
        self.assertTrue(argv[-1].startswith("sudo -n /usr/bin/python3 - native --engine bind --zone "))
        self.assertIn("'a b.example'", argv[-1])
        self.assertEqual(stdin, guests.probe_bytes())
        self.assertNotIn(b"\r", stdin)

    def test_tunnel_argv_and_failure(self) -> None:
        captured = {}

        class Process:
            returncode = 255

            def poll(self):  # noqa: ANN201
                return 255

            def terminate(self) -> None:
                captured["terminated"] = True

            def wait(self, timeout=None):  # noqa: ANN001, ANN201
                return 255

        def popen(argv, **kwargs):  # noqa: ANN001, ANN003
            captured["argv"] = argv
            return Process()

        guest = guests.Guests(self.plan, Path("/nonexistent/id"), popen=popen)
        with self.assertRaises(guests.GuestError):
            with guest.tunnel("debian13", 28443):
                pass
        argv = captured["argv"]
        self.assertEqual(argv[0], "ssh")
        self.assertIn("ExitOnForwardFailure=yes", argv)
        self.assertIn("127.0.0.1:28443:127.0.0.1:2083", argv)
        self.assertEqual(argv[-1], "celik@127.0.0.1")
        self.assertTrue(captured["terminated"])


class StubDNS:
    """Loopback authoritative stub: SOA/A for one zone, REFUSED otherwise, and a catalog AXFR."""

    zone = "pair-accept.example"

    def __init__(self) -> None:
        self.udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.udp.bind(("127.0.0.1", 0))
        self.port = self.udp.getsockname()[1]
        self.tcp = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.tcp.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.tcp.bind(("127.0.0.1", self.port))
        self.tcp.listen(4)
        self.threads = [threading.Thread(target=self.serve_udp, daemon=True),
                        threading.Thread(target=self.serve_tcp, daemon=True)]
        for thread in self.threads:
            thread.start()

    def answer(self, packet: bytes) -> list[bytes]:
        query_id = struct.unpack("!H", packet[:2])[0]
        name, offset = gp.decode_name(packet, 12)
        qtype = struct.unpack("!H", packet[offset:offset + 2])[0]
        question = packet[12:offset + 4]
        soa = gp.encode_name("ns1.ns-accept.example") + gp.encode_name("h.example") + struct.pack("!IIIII", 7, 1, 2, 3, 4)

        def message(answers: list[bytes], rcode: int = 0, aa: bool = True) -> bytes:
            flags = 0x8000 | (0x0400 if aa else 0) | rcode
            return struct.pack("!HHHHHH", query_id, flags, 1, len(answers), 0, 0) + question + b"".join(answers)

        def rr(owner: str, rtype: int, rdata: bytes) -> bytes:
            return gp.encode_name(owner) + struct.pack("!HHIH", rtype, 1, 60, len(rdata)) + rdata

        if name == CATALOG + "." and qtype == 252:
            catalog_soa = rr(CATALOG, 6, soa)
            member = rr("abc.zones." + CATALOG, 12, gp.encode_name(self.zone))
            return [message([catalog_soa, member]), message([catalog_soa])]
        if name == self.zone + "." and qtype == 6:
            return [message([rr(self.zone, 6, soa)])]
        if name == "accept." + self.zone + "." and qtype == 1:
            return [message([rr("accept." + self.zone, 1, bytes([198, 51, 100, 10]))])]
        return [message([], rcode=5, aa=False)]

    def serve_udp(self) -> None:
        while True:
            try:
                packet, peer = self.udp.recvfrom(4096)
            except OSError:
                return
            self.udp.sendto(self.answer(packet)[0], peer)

    def serve_tcp(self) -> None:
        while True:
            try:
                connection, _ = self.tcp.accept()
            except OSError:
                return
            with connection:
                size = struct.unpack("!H", connection.recv(2))[0]
                packet = b""
                while len(packet) < size:
                    packet += connection.recv(size - len(packet))
                for reply in self.answer(packet):
                    connection.sendall(struct.pack("!H", len(reply)) + reply)

    def close(self) -> None:
        self.udp.close()
        self.tcp.close()


class LiveProbeTest(unittest.TestCase):
    def setUp(self) -> None:
        self.stub = StubDNS()
        self.addCleanup(self.stub.close)

    def test_udp_and_tcp_answers(self) -> None:
        for tcp in (False, True):
            soa = gp.query("127.0.0.1", StubDNS.zone, "SOA", tcp=tcp, port=self.stub.port)
            self.assertEqual((soa["rcode"], soa["authoritative"]), ("NOERROR", True), soa)
            self.assertEqual(soa["answer"][0]["data"].split()[2], "7")
            a = gp.query("127.0.0.1", "accept." + StubDNS.zone, "A", tcp=tcp, port=self.stub.port)
            self.assertEqual(a["answer"][0]["data"], "198.51.100.10")
            refused = gp.query("127.0.0.1", "other.example", "SOA", tcp=tcp, port=self.stub.port)
            self.assertEqual((refused["rcode"], refused["authoritative"]), ("REFUSED", False))

    def test_catalog_axfr_members(self) -> None:
        out = gp.catalog_command("127.0.0.1", CATALOG, port=self.stub.port)
        self.assertTrue(out["transferred"], out)
        self.assertEqual(out["serial"], 7)
        self.assertEqual(out["members"], [StubDNS.zone + "."])

    def test_unreachable_server_is_an_error_not_absence(self) -> None:
        closed = socket.socket()
        closed.bind(("127.0.0.1", 0))
        port = closed.getsockname()[1]
        closed.close()
        out = gp.query("127.0.0.1", StubDNS.zone, "SOA", tcp=True, port=port, timeout=1)
        self.assertIn("error", out)
        catalog = gp.catalog_command("127.0.0.1", CATALOG, timeout=1, port=port)
        self.assertFalse(catalog["transferred"])


if __name__ == "__main__":
    unittest.main()
