#!/usr/bin/env python3
"""Behavior tests for the bounded public portal verifier."""

from __future__ import annotations

import contextlib
import hashlib
import importlib.util
import io
import json
import os
import shutil
import sys
import tempfile
import threading
import unittest
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


SCRIPT = Path(__file__).with_name("verify-download-portal-public.py")
SPEC = importlib.util.spec_from_file_location("download_portal_public_verifier", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("cannot import public portal verifier")
VERIFIER = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = VERIFIER
SPEC.loader.exec_module(VERIFIER)


VERSION = "v9.8.7-alpha.6"
OS_NAME = "linux"
ARCHITECTURE = "amd64"
COMMIT = "0123456789abcdef0123456789abcdef01234567"
SEQUENCE = "106"
PUBLISHED_AT = "2026-08-26T01:02:03Z"
ARCHIVE_NAME = f"celikpanel-{VERSION}-{OS_NAME}-{ARCHITECTURE}.tar.gz"
PLATFORM_PREFIX = f"releases/{VERSION}/{OS_NAME}/{ARCHITECTURE}"
ARCHIVE_PUBLIC_PATH = f"/{PLATFORM_PREFIX}/{ARCHIVE_NAME}"


def write_bytes(root: Path, relative: str, data: bytes) -> Path:
    destination = root.joinpath(*relative.split("/"))
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(data)
    return destination


def json_bytes(value: object) -> bytes:
    return (json.dumps(value, indent=2) + "\n").encode("utf-8")


def build_site(root: Path) -> tuple[int, set[str], int]:
    root_files = {
        "index.html": b"<!doctype html><title>CelikPanel</title>\n",
        "technical.html": b"<!doctype html><title>Technical details</title>\n",
        "assets/favicon-v2.svg": b'<svg xmlns="http://www.w3.org/2000/svg"/>\n',
        "assets/site.css": b"body { color: #10213a; }\n",
        "assets/site.js": b"document.documentElement.dataset.ready = 'true';\n",
        "get.sh": b"#!/bin/sh\nprintf 'download\\n'\n",
        "release-signing-ed25519.pem": (
            b"-----BEGIN PUBLIC KEY-----\ntest-key\n-----END PUBLIC KEY-----\n"
        ),
        ".well-known/security.txt": b"Contact: mailto:security@example.test\n",
    }
    # Tiny valid WebP fixture: compare binary image bytes as well as HTML.
    webp = bytes.fromhex(
        "524946461e000000574542505650384c110000002f0000000007508c3a94a4ff8188e87f0000"
    )
    for feature in ("overview", "domains", "databases"):
        for language in ("tr", "en"):
            root_files[f"assets/product-{feature}-{language}.webp"] = webp
    for relative, data in root_files.items():
        write_bytes(root, relative, data)

    archive = bytes(range(256)) * 1024
    archive_sha256 = hashlib.sha256(archive).hexdigest()
    archive_size = len(archive)
    archive_url = f"/{PLATFORM_PREFIX}/{ARCHIVE_NAME}"
    latest = {
        "version": VERSION,
        "sequence": SEQUENCE,
        "commit": COMMIT,
        "published_at": PUBLISHED_AT,
        "os": OS_NAME,
        "arch": ARCHITECTURE,
        "sha256": archive_sha256,
        "archive_url": archive_url,
        "checksum_url": archive_url + ".sha256",
        "signed_manifest_url": f"/{PLATFORM_PREFIX}/release-manifest-v2",
        "signed_manifest_signature_url": f"/{PLATFORM_PREFIX}/release-manifest-v2.sig",
    }
    latest_raw = json_bytes(latest)
    write_bytes(root, "releases/latest.txt", f"{VERSION}\n".encode("ascii"))
    write_bytes(root, "releases/latest.json", latest_raw)
    write_bytes(
        root,
        "releases/index.json",
        json_bytes({"latest": VERSION, "releases": [latest]}),
    )
    write_bytes(root, f"{PLATFORM_PREFIX}/release.json", latest_raw)
    write_bytes(root, f"{PLATFORM_PREFIX}/{ARCHIVE_NAME}", archive)
    write_bytes(
        root,
        f"{PLATFORM_PREFIX}/{ARCHIVE_NAME}.sha256",
        f"{archive_sha256}  {ARCHIVE_NAME}\n".encode("ascii"),
    )
    manifest = (
        "format=celikpanel-release-manifest-v2\n"
        f"sequence={SEQUENCE}\n"
        f"version={VERSION}\n"
        f"commit={COMMIT}\n"
        f"published_at={PUBLISHED_AT}\n"
        f"os={OS_NAME}\n"
        f"arch={ARCHITECTURE}\n"
        f"archive={ARCHIVE_NAME}\n"
        f"archive_sha256={archive_sha256}\n"
        f"archive_size={archive_size}\n"
    ).encode("ascii")
    write_bytes(root, f"{PLATFORM_PREFIX}/release-manifest-v2", manifest)
    write_bytes(root, f"{PLATFORM_PREFIX}/release-manifest-v2.sig", bytes(range(64)))

    legacy_archive_name = f"celikpanel-{VERSION}.tar.gz"
    legacy = {
        "version": VERSION,
        "commit": COMMIT,
        "published_at": PUBLISHED_AT,
        "sha256": archive_sha256,
        "archive_url": f"/releases/{VERSION}/{legacy_archive_name}",
        "checksum_url": f"/releases/{VERSION}/{legacy_archive_name}.sha256",
    }
    write_bytes(root, f"releases/{VERSION}/release.json", json_bytes(legacy))
    write_bytes(root, f"releases/{VERSION}/{legacy_archive_name}", archive)
    write_bytes(
        root,
        f"releases/{VERSION}/{legacy_archive_name}.sha256",
        f"{archive_sha256}  {legacy_archive_name}\n".encode("ascii"),
    )

    historical_paths: set[str] = set()
    historical_logical_size = 0
    for number in range(1000):
        historical_version = f"v0.0.0-history.{number}"
        historical_name = f"celikpanel-{historical_version}.tar.gz"
        relative = f"releases/{historical_version}/{historical_name}"
        path = write_bytes(root, relative, b"")
        with path.open("r+b") as sparse:
            sparse.truncate(4 * 1024 * 1024)
        historical_paths.add("/" + relative)
        historical_logical_size += path.stat().st_size
    return archive_size, historical_paths, historical_logical_size


class RecordingHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format, *args):  # noqa: A003
        return

    def do_GET(self):  # noqa: N802
        path = urllib.parse.unquote(urllib.parse.urlsplit(self.path).path)
        self.server.request_paths.append(path)
        if path == self.server.redirect_path:
            self.send_response(302)
            self.send_header("Location", "/redirect-target")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if path in self.server.historical_paths:
            self.server.historical_requests.append(path)
            self.send_response(418)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        member_body = getattr(self.server, "membership_body", None)
        if path == "/account/index.php" and member_body is not None:
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=UTF-8")
            self.send_header("Cache-Control", "no-store")
            if self.server.membership_cookie:
                self.send_header("Set-Cookie", "__Host-celikpanel_member=test; Path=/; Secure; HttpOnly; SameSite=Lax")
            self.send_header("Content-Length", str(len(member_body)))
            self.end_headers()
            self.wfile.write(member_body)
            self.server.served_bytes += len(member_body)
            return
        relative = path.removeprefix("/")
        candidate = self.server.site.joinpath(*relative.split("/"))
        try:
            candidate.resolve(strict=True).relative_to(
                self.server.site.resolve(strict=True)
            )
        except (OSError, ValueError):
            self.send_error(404)
            return
        if not candidate.is_file():
            self.send_error(404)
            return
        size = candidate.stat().st_size
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        if path == self.server.content_encoding_path:
            self.send_header("Content-Encoding", "identity")
        if path == self.server.transfer_encoding_path:
            self.send_header("Transfer-Encoding", "chunked")
        length_mode = (
            self.server.content_length_mode
            if path == self.server.content_length_path
            else "exact"
        )
        if length_mode == "exact":
            self.send_header("Content-Length", str(size))
        elif length_mode == "wrong":
            self.send_header("Content-Length", str(size + 1))
        elif length_mode == "duplicate":
            self.send_header("Content-Length", str(size))
            self.send_header("Content-Length", str(size))
        elif length_mode != "missing":
            raise AssertionError(f"unknown Content-Length mode: {length_mode}")
        self.end_headers()
        tamper = path == self.server.tamper_path
        offset = 0
        with candidate.open("rb") as source:
            while True:
                chunk = source.read(64 * 1024)
                if not chunk:
                    break
                if tamper and offset == 0:
                    chunk = bytes([chunk[0] ^ 1]) + chunk[1:]
                try:
                    self.wfile.write(chunk)
                except (BrokenPipeError, ConnectionResetError):
                    return
                self.server.served_bytes += len(chunk)
                offset += len(chunk)


class PublicPortalVerifierTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        cls.site = Path(cls.temporary.name) / "site"
        cls.site.mkdir()
        (
            cls.archive_size,
            cls.historical_paths,
            cls.historical_logical_size,
        ) = build_site(cls.site)
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), RecordingHandler)
        cls.server.site = cls.site
        cls.server.historical_paths = cls.historical_paths
        cls.server.request_paths = []
        cls.server.historical_requests = []
        cls.server.served_bytes = 0
        cls.server.tamper_path = None
        cls.server.redirect_path = None
        cls.server.content_encoding_path = None
        cls.server.transfer_encoding_path = None
        cls.server.content_length_path = None
        cls.server.content_length_mode = "exact"
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=5)
        cls.temporary.cleanup()

    def setUp(self):
        self.server.request_paths.clear()
        self.server.historical_requests.clear()
        self.server.served_bytes = 0
        self.server.tamper_path = None
        self.server.redirect_path = None
        self.server.content_encoding_path = None
        self.server.transfer_encoding_path = None
        self.server.content_length_path = None
        self.server.content_length_mode = "exact"

    def verify(self):
        return VERIFIER.verify_public_portal(
            self.site,
            self.base_url,
            VERSION,
            timeout=5,
        )

    def membership_fixture(self, body, cookie=True):
        self.server.membership_body = body
        self.server.membership_cookie = cookie
        for relative in ("account/index.php", "account/member.css", "account/member.js"):
            write_bytes(self.site, relative, b"fixture")
        def cleanup():
            self.server.membership_body = None
            for name in ("index.php", "member.css", "member.js"):
                (self.site / "account" / name).unlink()
            (self.site / "account").rmdir()
        self.addCleanup(cleanup)

    def test_membership_extends_one_plan_without_fetching_php_source(self):
        self.membership_fixture(b'<body class="member-page"><input name="csrf" value="' + b'a'*64 + b'"></body>')
        summary = self.verify()
        self.assertEqual(summary["requests"], 26)
        self.assertEqual(summary["archive_gets"], 1)
        self.assertEqual(self.server.request_paths.count("/account/index.php"), 1)

    def test_membership_refuses_unexecuted_php(self):
        self.membership_fixture(b'<?php require "private-app";')
        with self.assertRaisesRegex(VERIFIER.VerificationError, "PHP handler"):
            self.verify()
        self.assertNotIn(ARCHIVE_PUBLIC_PATH, self.server.request_paths)

    def test_membership_refuses_insecure_session(self):
        self.membership_fixture(b'<body class="member-page"><input name="csrf" value="' + b'a'*64 + b'"></body>', cookie=False)
        with self.assertRaisesRegex(VERIFIER.VerificationError, "secure session"):
            self.verify()

    def test_single_pass_is_bounded_and_never_requests_history(self):
        summary = self.verify()
        self.assertEqual(summary["status"], "ok")
        self.assertEqual(summary["policy"], "single-full-pass-after-exchange")
        self.assertEqual(summary["phase"], "full")
        self.assertEqual(summary["requests"], 23)
        self.assertEqual(summary["request_limit"], VERIFIER.HARD_REQUEST_LIMIT)
        expected_visual_paths = {
            "/technical.html", "/assets/favicon-v2.svg",
            *(f"/assets/product-{feature}-{language}.webp"
              for feature in ("overview", "domains", "databases")
              for language in ("tr", "en")),
        }
        self.assertTrue(expected_visual_paths.issubset(self.server.request_paths))
        self.assertEqual(len(set(self.server.request_paths)), 23)
        self.assertEqual(summary["requests"], len(self.server.request_paths))
        self.assertEqual(summary["archive_gets"], 1)
        self.assertEqual(self.server.request_paths.count(ARCHIVE_PUBLIC_PATH), 1)
        self.assertEqual(self.server.historical_requests, [])
        self.assertTrue(self.historical_paths.isdisjoint(self.server.request_paths))
        self.assertGreater(self.historical_logical_size, 4_000_000_000)
        self.assertEqual(summary["downloaded_bytes"], self.server.served_bytes)
        self.assertLessEqual(
            summary["downloaded_bytes"],
            self.archive_size + 1024 * 1024,
        )
        self.assertEqual(
            summary["download_byte_limit"],
            self.archive_size + 1024 * 1024,
        )
        legacy_archive = f"/releases/{VERSION}/celikpanel-{VERSION}.tar.gz"
        self.assertNotIn(legacy_archive, self.server.request_paths)

    def test_public_tampering_fails_closed(self):
        self.server.tamper_path = "/releases/latest.json"
        with self.assertRaisesRegex(VERIFIER.VerificationError, "public bytes differ"):
            self.verify()
        self.assertNotIn(ARCHIVE_PUBLIC_PATH, self.server.request_paths)
        self.assertEqual(self.server.historical_requests, [])

    def test_visual_asset_tampering_fails_before_archive(self):
        for relative in ("technical.html", "assets/favicon-v2.svg", *(
            f"assets/product-{feature}-{language}.webp"
            for feature in ("overview", "domains", "databases")
            for language in ("tr", "en")
        )):
            with self.subTest(relative=relative):
                self.server.request_paths.clear()
                self.server.tamper_path = "/" + relative
                with self.assertRaisesRegex(VERIFIER.VerificationError, "public bytes differ"):
                    self.verify()
                self.assertNotIn(ARCHIVE_PUBLIC_PATH, self.server.request_paths)

    def test_missing_screenshot_fails_before_network(self):
        target = self.site / "assets/product-databases-en.webp"
        original = target.read_bytes()
        try:
            target.unlink()
            with self.assertRaisesRegex(VERIFIER.VerificationError, "cannot stat"):
                self.verify()
            self.assertEqual(self.server.request_paths, [])
        finally:
            target.write_bytes(original)

    def test_static_budget_overrun_fails_before_network(self):
        target = self.site / "assets/product-overview-tr.webp"
        original = target.read_bytes()
        try:
            target.write_bytes(b"x" * VERIFIER.SMALL_RESPONSE_BUDGET)
            with self.assertRaisesRegex(VERIFIER.VerificationError, "network budget"):
                self.verify()
            self.assertEqual(self.server.request_paths, [])
        finally:
            target.write_bytes(original)

    def test_archive_tampering_fails_after_exactly_one_archive_get(self):
        self.server.tamper_path = ARCHIVE_PUBLIC_PATH
        with self.assertRaisesRegex(VERIFIER.VerificationError, "public bytes differ"):
            self.verify()
        self.assertEqual(self.server.request_paths.count(ARCHIVE_PUBLIC_PATH), 1)
        self.assertEqual(self.server.historical_requests, [])

    def test_redirect_is_not_followed(self):
        self.server.redirect_path = "/index.html"
        with self.assertRaisesRegex(VERIFIER.VerificationError, "redirect"):
            self.verify()
        self.assertEqual(self.server.request_paths, ["/index.html"])
        self.assertNotIn("/redirect-target", self.server.request_paths)

    def test_any_content_encoding_is_refused(self):
        self.server.content_encoding_path = "/index.html"
        with self.assertRaisesRegex(VERIFIER.VerificationError, "encoded response"):
            self.verify()
        self.assertEqual(self.server.request_paths, ["/index.html"])

    def test_content_length_must_be_present_exact_and_unique(self):
        for mode in ("missing", "wrong", "duplicate"):
            with self.subTest(mode=mode):
                self.server.request_paths.clear()
                self.server.content_length_path = "/index.html"
                self.server.content_length_mode = mode
                with self.assertRaisesRegex(
                    VERIFIER.VerificationError, "Content-Length"
                ):
                    self.verify()
                self.assertEqual(self.server.request_paths, ["/index.html"])

    def test_transfer_encoding_is_refused(self):
        self.server.transfer_encoding_path = "/index.html"
        self.server.content_length_path = "/index.html"
        self.server.content_length_mode = "missing"
        with self.assertRaisesRegex(VERIFIER.VerificationError, "transfer-encoded"):
            self.verify()
        self.assertEqual(self.server.request_paths, ["/index.html"])

    def test_final_and_ancestor_symlinks_are_refused_before_network(self):
        for relative in ("release-signing-ed25519.pem", "assets"):
            with self.subTest(relative=relative):
                original = self.site / relative
                saved = original.with_name(original.name + ".saved-for-symlink-test")
                original.rename(saved)
                try:
                    try:
                        os.symlink(saved, original, target_is_directory=saved.is_dir())
                    except (OSError, NotImplementedError) as exc:
                        self.skipTest(f"symlinks are unavailable: {exc}")
                    with self.assertRaisesRegex(
                        VERIFIER.VerificationError, "symlink"
                    ):
                        self.verify()
                    self.assertEqual(self.server.request_paths, [])
                finally:
                    if original.is_symlink():
                        original.unlink()
                    saved.rename(original)

    def test_local_inode_swap_after_plan_is_refused(self):
        inner = VERIFIER.urllib.request.build_opener(
            VERIFIER.urllib.request.ProxyHandler({}),
            VERIFIER.NoRedirect(),
        )
        target = self.site / "index.html"

        class SwappingOpener:
            swapped = False

            def open(swapper, request, timeout):
                if not swapper.swapped:
                    replacement = target.with_name("index.html.replacement")
                    shutil.copyfile(target, replacement)
                    os.replace(replacement, target)
                    swapper.swapped = True
                return inner.open(request, timeout=timeout)

        with self.assertRaisesRegex(
            VERIFIER.VerificationError, "identity changed"
        ):
            VERIFIER.verify_public_portal(
                self.site,
                self.base_url,
                VERSION,
                opener=SwappingOpener(),
                timeout=5,
            )
        self.assertEqual(self.server.request_paths, ["/index.html"])

    def test_transaction_has_no_second_public_phase(self):
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr), self.assertRaises(SystemExit):
            VERIFIER.parse_args(
                [
                    "--site",
                    os.fspath(self.site),
                    "--base-url",
                    self.base_url,
                    "--target-version",
                    VERSION,
                    "--phase",
                    "selectors",
                ]
            )
        self.assertIn("invalid choice", stderr.getvalue())
        self.assertEqual(self.server.request_paths, [])

    def test_wrong_pinned_version_fails_before_network(self):
        with self.assertRaisesRegex(
            VERIFIER.VerificationError, "not the pinned version"
        ):
            VERIFIER.verify_public_portal(
                self.site,
                self.base_url,
                "v9.8.7-alpha.5",
                timeout=5,
            )
        self.assertEqual(self.server.request_paths, [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
