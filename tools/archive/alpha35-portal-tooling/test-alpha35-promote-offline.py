#!/usr/bin/env python3
import importlib.util
import os
import shutil
import tempfile
from pathlib import Path


script = Path("/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/promote-alpha35-portal.py")
package = Path("/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/celikpanel-alpha35-portal-31c28e9-seq35.tar.gz")
spec = importlib.util.spec_from_file_location("alpha35_promote", script)
module = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(module)

expected_old_versions = {
    *(f"v0.1.0-alpha.{number}" for number in range(9, 29)),
    "v0.1.0-alpha.30",
    "v0.1.0-alpha.31",
    "v0.1.0-alpha.32",
    "v0.1.0-alpha.33",
    "v0.1.0-alpha.34",
}
assert module.VERSION == "v0.1.0-alpha.35"
assert module.PREVIOUS == "v0.1.0-alpha.34"
assert module.SEQUENCE == "35"
assert module.COMMIT == "31c28e941ddcde5cb0980ac471910bd98f6e1984"
assert module.PACKAGE_SIZE == 44_457_897
assert module.PACKAGE_SHA == "a288f9a1be381552f59a35a41c666d2e1aee989e72715c78259d10c9b6016256"
assert module.OLD_VERSIONS == expected_old_versions

module.UPLOAD = package
stage = Path(tempfile.mkdtemp(prefix="alpha35-promote-offline-"))
try:
    module.validate_archive_and_extract(stage)
    assert module.inventory(stage) == module.expected_initial_inventory()
    module.validate_selectors(stage)
    for relative, expected in module.EXPECTED_RELEASE_DIGESTS.items():
        assert module.sha256(stage / relative) == expected
    module.LIVE = stage
    seen = []

    def exact_fetch(relative):
        seen.append(relative)
        return (stage / relative).read_bytes()

    module.fetch = exact_fetch
    module.post_publish_verify([])
    required_public = {"get.sh", "assets/site.js", "assets/site.css"}
    assert required_public.issubset(seen)
    assert ".htaccess" not in seen

    def corrupt_get(relative):
        payload = (stage / relative).read_bytes()
        return payload + b"\n" if relative == "get.sh" else payload

    module.fetch = corrupt_get
    try:
        module.post_publish_verify([])
    except RuntimeError as error:
        assert str(error) == "public byte mismatch: get.sh"
    else:
        raise AssertionError("get.sh exact-byte verification did not fail closed")
finally:
    shutil.rmtree(stage)
fault_root = Path(tempfile.mkdtemp(prefix="alpha35-preexchange-fault-"))
try:
    live = fault_root / "httpdocs"
    backups = fault_root / "portal-backups"
    releases = live / "releases"
    upload_dir = fault_root / ".upload-alpha35-fault"
    upload = upload_dir / "portal.tar.gz"
    lock = fault_root / ".portal-deploy.lock"
    stale_siblings = [
        fault_root / ".upload-alpha13-20260812T132500Z",
        fault_root / ".upload-alpha14-20260813T0825Z",
        fault_root / ".upload-alpha16-20260813T1919Z",
    ]
    releases.mkdir(parents=True)
    backups.mkdir()
    upload_dir.mkdir(mode=0o700)
    for stale in stale_siblings:
        stale.mkdir(mode=0o700)
    os.chmod(live, 0o755)
    os.chmod(upload_dir, 0o700)
    upload.write_bytes(b"fault-upload")
    os.chmod(upload, 0o600)
    lock.write_bytes(b"")
    os.chmod(lock, 0o600)
    for version in module.OLD_VERSIONS:
        (releases / version).mkdir()
    (releases / "latest.txt").write_text(module.PREVIOUS + "\n", encoding="utf-8")
    (releases / "latest.json").write_text("{}\n", encoding="utf-8")
    (releases / "index.json").write_text("{}\n", encoding="utf-8")

    module.ROOT = fault_root
    module.LIVE = live
    module.BACKUPS = backups
    module.UPLOAD_DIR = upload_dir
    module.UPLOAD = upload
    module.LOCK = lock
    module.PRESERVED_UPLOAD_DIRS = tuple(stale_siblings)
    stale_metadata = {path: os.lstat(path) for path in stale_siblings}
    live_id = module.identity(live)
    sentinel = RuntimeError("injected pre-exchange extraction failure")
    module.inventory = lambda _path: []

    def fail_during_extract(stage_path):
        (stage_path / "partial").write_bytes(b"partial")
        raise sentinel

    module.validate_archive_and_extract = fail_during_extract
    try:
        module.main()
    except RuntimeError as error:
        assert error is sentinel
    else:
        raise AssertionError("pre-exchange fault was not reraised")

    assert module.identity(live) == live_id
    assert not upload_dir.exists()
    assert all(stale.is_dir() for stale in stale_siblings)
    assert {path: os.lstat(path) for path in stale_siblings} == stale_metadata
    assert not any(fault_root.glob(".stage-alpha35-*"))
    assert module.fcntl is not None
    probe_fd = os.open(lock, os.O_RDWR | os.O_CLOEXEC)
    try:
        module.fcntl.flock(probe_fd, module.fcntl.LOCK_EX | module.fcntl.LOCK_NB)
        module.fcntl.flock(probe_fd, module.fcntl.LOCK_UN)
    finally:
        os.close(probe_fd)
finally:
    shutil.rmtree(fault_root)

# Exercise a real post-exchange failure and require an automatic inode-preserving rollback.
import ast
import hashlib
import tarfile


def load_fresh(name):
    fresh_spec = importlib.util.spec_from_file_location(name, script)
    fresh = importlib.util.module_from_spec(fresh_spec)
    assert fresh_spec.loader is not None
    fresh_spec.loader.exec_module(fresh)
    return fresh


def stale_snapshot(paths):
    snapshot = {}
    for root in paths:
        entries = []
        for item in [root, *sorted(root.rglob("*"))]:
            value = os.lstat(item)
            relative = "." if item == root else item.relative_to(root).as_posix()
            payload = item.read_bytes() if item.is_file() and not item.is_symlink() else None
            entries.append((relative, tuple(value), value.st_mtime_ns, value.st_ctime_ns, payload))
        snapshot[root] = entries
    return snapshot


rollback_module = load_fresh("alpha35_promote_rollback")
rollback_root = Path(tempfile.mkdtemp(prefix="alpha35-postexchange-rollback-"))
try:
    live = rollback_root / "httpdocs"
    backups = rollback_root / "portal-backups"
    releases = live / "releases"
    upload_dir = rollback_root / ".upload-alpha35-rollback"
    upload = upload_dir / "portal.tar.gz"
    lock = rollback_root / ".portal-deploy.lock"
    stale_siblings = [
        rollback_root / ".upload-alpha13-20260812T132500Z",
        rollback_root / ".upload-alpha14-20260813T0825Z",
        rollback_root / ".upload-alpha16-20260813T1919Z",
    ]
    releases.mkdir(parents=True)
    backups.mkdir()
    upload_dir.mkdir(mode=0o700)
    os.chmod(live, 0o755)
    os.chmod(backups, 0o755)
    os.chmod(upload_dir, 0o700)
    shutil.copy2(package, upload)
    os.chmod(upload, 0o600)
    lock.write_bytes(b"")
    os.chmod(lock, 0o600)
    for version in rollback_module.OLD_VERSIONS:
        release = releases / version
        release.mkdir(mode=0o755)
        os.chmod(release, 0o755)
        (release / "historical.txt").write_text(version + "\n", encoding="utf-8")
        os.chmod(release / "historical.txt", 0o644)
    (releases / "latest.txt").write_text(rollback_module.PREVIOUS + "\n", encoding="utf-8")
    (releases / "latest.json").write_text("{}\n", encoding="utf-8")
    (releases / "index.json").write_text("{}\n", encoding="utf-8")
    for selector in (releases / "latest.txt", releases / "latest.json", releases / "index.json"):
        os.chmod(selector, 0o644)
    for number, stale in enumerate(stale_siblings, start=13):
        stale.mkdir(mode=0o700)
        os.chmod(stale, 0o700)
        (stale / "preserve.bin").write_bytes(f"stale-{number}".encode("ascii"))
        os.chmod(stale / "preserve.bin", 0o600)

    rollback_module.ROOT = rollback_root
    rollback_module.LIVE = live
    rollback_module.BACKUPS = backups
    rollback_module.UPLOAD_DIR = upload_dir
    rollback_module.UPLOAD = upload
    rollback_module.LOCK = lock
    rollback_module.PRESERVED_UPLOAD_DIRS = tuple(stale_siblings)
    live_id = rollback_module.identity(live)
    live_before = rollback_module.inventory(live)
    stale_before = stale_snapshot(stale_siblings)
    injected = RuntimeError("injected post-exchange public verification failure")
    calls = []

    def fail_after_exchange(old_public_paths):
        calls.append(tuple(old_public_paths))
        raise injected

    rollback_module.post_publish_verify = fail_after_exchange
    try:
        rollback_module.main()
    except RuntimeError as error:
        assert error is injected
    else:
        raise AssertionError("post-exchange fault was not reraised")

    assert len(calls) == 1
    assert rollback_module.identity(live) == live_id
    assert rollback_module.inventory(live) == live_before
    assert not upload_dir.exists()
    assert stale_snapshot(stale_siblings) == stale_before
    assert not any(rollback_root.glob(".stage-alpha35-*"))
    assert not any(rollback_root.glob(".failed-alpha35-*"))
    assert list(backups.iterdir()) == []
    probe_fd = os.open(lock, os.O_RDWR | os.O_CLOEXEC)
    try:
        rollback_module.fcntl.flock(probe_fd, rollback_module.fcntl.LOCK_EX | rollback_module.fcntl.LOCK_NB)
        rollback_module.fcntl.flock(probe_fd, rollback_module.fcntl.LOCK_UN)
    finally:
        os.close(probe_fd)
finally:
    shutil.rmtree(rollback_root)

# Cleanup must fail closed if an attacker swaps the uploaded inode.
toctou_module = load_fresh("alpha35_promote_toctou")
toctou_root = Path(tempfile.mkdtemp(prefix="alpha35-toctou-"))
try:
    upload_dir = toctou_root / ".upload-alpha35-toctou"
    upload_dir.mkdir(mode=0o700)
    os.chmod(upload_dir, 0o700)
    upload = upload_dir / "portal.tar.gz"
    upload.write_bytes(b"original")
    os.chmod(upload, 0o600)
    old_upload_id = (os.lstat(upload).st_dev, os.lstat(upload).st_ino)
    upload.unlink()
    upload.write_bytes(b"replacement")
    os.chmod(upload, 0o600)
    toctou_module.ROOT = toctou_root
    toctou_module.UPLOAD_DIR = upload_dir
    toctou_module.UPLOAD = upload
    try:
        toctou_module.cleanup_upload(old_upload_id)
    except RuntimeError as error:
        assert str(error) == "upload identity changed before cleanup"
    else:
        raise AssertionError("replaced upload inode was deleted")
    assert upload.read_bytes() == b"replacement"

    candidate = toctou_root / ".stage-alpha35-safe"
    candidate.mkdir()
    candidate_id = toctou_module.identity(candidate)
    toctou_module.validate_unique_candidate(candidate, ".stage-alpha35-", candidate_id)
    try:
        toctou_module.validate_unique_candidate(candidate, ".failed-alpha35-", candidate_id)
    except RuntimeError as error:
        assert "unsafe cleanup target" in str(error)
    else:
        raise AssertionError("wrong cleanup prefix was accepted")
    try:
        toctou_module.validate_unique_candidate(candidate, ".stage-alpha35-", (candidate_id[0], candidate_id[1] + 1))
    except RuntimeError as error:
        assert "cleanup identity mismatch" in str(error)
    else:
        raise AssertionError("wrong cleanup inode was accepted")
finally:
    shutil.rmtree(toctou_root)

# Tar traversal and filesystem symlink inputs must fail closed.
traversal = tarfile.TarInfo("portal/../escape")
traversal.type = tarfile.REGTYPE
try:
    module.normalized_tar_name(traversal)
except RuntimeError as error:
    assert "noncanonical tar path" in str(error)
else:
    raise AssertionError("tar traversal path was accepted")

symlink_root = Path(tempfile.mkdtemp(prefix="alpha35-symlink-gate-"))
try:
    target = symlink_root / "target"
    target.write_bytes(b"target")
    (symlink_root / "link").symlink_to(target)
    try:
        module.safe_tree(symlink_root)
    except RuntimeError as error:
        assert "unsafe tree member" in str(error)
    else:
        raise AssertionError("filesystem symlink was accepted")
finally:
    shutil.rmtree(symlink_root)

# Static parser and publisher gates; this test never invokes the publisher.
ast.parse(script.read_text(encoding="utf-8"), filename=str(script))
ast.parse(Path(__file__).read_text(encoding="utf-8"), filename=__file__)
publisher_path = Path("/mnt/c/tmp/celikpanel-alpha35-portal-candidate-run32518010435/publish-alpha35-portal.ps1")
publisher = publisher_path.read_text(encoding="utf-8")
assert publisher.count("UpdateHostKeys=no") == 4
assert "UpdateHostKeys=yes" not in publisher
assert ".upload-*" not in publisher
assert "rm -" not in publisher
assert "Remove-Item" not in publisher
for stale_name in (
    ".upload-alpha13-20260812T132500Z",
    ".upload-alpha14-20260813T0825Z",
    ".upload-alpha16-20260813T1919Z",
):
    assert stale_name in publisher
assert "$PackageSize = 44457897" in publisher
assert "a288f9a1be381552f59a35a41c666d2e1aee989e72715c78259d10c9b6016256" in publisher
assert "$PromotionScriptSize = 26937" in publisher
assert "087be6fb989e8ee933703f960d521820f30b95c07cce38cb386ae09d33f535ee" in publisher
assert "test \"$(cat -- \"$live/releases/latest.txt\")\" = v0.1.0-alpha.34" in publisher
assert "test ! -e \"$live/releases/v0.1.0-alpha.35\"" in publisher
assert "= 28" in publisher
assert hashlib.sha256(package.read_bytes()).hexdigest() == module.PACKAGE_SHA
assert package.stat().st_size == module.PACKAGE_SIZE

print("ALPHA35_PREEXCHANGE_FAULT_CLEANUP_PASS")
print("ALPHA35_POSTEXCHANGE_ROLLBACK_PASS")
print("ALPHA35_TOCTOU_AND_PARSER_GATES_PASS")
print("ALPHA35_STALE_SIBLING_PRESERVATION_PASS")
print("ALPHA35_PROMOTION_OFFLINE_PACKAGE_PASS")
