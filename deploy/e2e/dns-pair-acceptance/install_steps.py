"""Guest command text for the customer installer and the owner enrollment CLI.

Installation uses the real ``install.sh`` from a locally built ``make dist``
archive, exactly as the accepted S-4 first-admin harness does
(``artifacts/s4-first-admin/harness/acceptance.py`` ``direct_stage`` /
``upload_credentials`` / ``direct_install``): the archive is staged under a
root-owned ``/var/backups/celikpanel/...`` chain (``install.sh`` refuses a
group/other-writable ancestor), the first administrator arrives as a root-only
credentials file that the installer consumes, and nothing is built on the
guest (a prebuilt tree without ``.git`` skips the build). Package prerequisites
come from the guest's normal distribution mirrors.

Secrets never appear in a command line: the archive and the credentials file
are streamed on SSH stdin.
"""

from __future__ import annotations

import json
import re
import shlex

STAGE_PARENT = "/var/backups/celikpanel/pair-accept"
CREDENTIAL_DIR = "/run/celikpanel-pair-accept"
CREDENTIAL_FILE = CREDENTIAL_DIR + "/admin.credentials"
DIST_ROOT_RE = re.compile(r"celikpanel-[A-Za-z0-9][A-Za-z0-9._+-]{0,80}")
HEX64 = re.compile(r"[0-9a-f]{64}")
HEX40 = re.compile(r"[0-9a-f]{40}")
OWNER_TOOLS = "dns-owner-tools"


class InstallError(ValueError):
    pass


def dist_root(root_name: str) -> str:
    if DIST_ROOT_RE.fullmatch(root_name) is None:
        raise InstallError(f"unexpected dist root name: {root_name!r}")
    return f"{STAGE_PARENT}/{root_name}"


def preinstall_check() -> str:
    """Fresh guest: no CelikPanel layout and no staged archive."""

    paths = ["/opt/celikpanel", "/var/lib/celikpanel", "/etc/celikpanel",
             "/var/lib/celikpanel-agent-private", "/etc/systemd/system/celikpanel-panel.service",
             "/etc/systemd/system/celikpanel-agent.service", STAGE_PARENT, CREDENTIAL_FILE]
    lines = ["set -euo pipefail"]
    for path in paths:
        lines.append(f"sudo -n test ! -e {path}")
        lines.append(f"sudo -n test ! -L {path}")
    lines.append("echo PREINSTALL_PRODUCT_LAYOUT=absent")
    return "\n".join(lines) + "\n"


def stage_archive(root_name: str, archive_sha256: str, commit: str, tree: str) -> str:
    """Receive the dist archive on stdin into a root-only staging chain."""

    if not HEX64.fullmatch(archive_sha256) or not HEX40.fullmatch(commit) or not HEX40.fullmatch(tree):
        raise InstallError("archive digest, commit and tree must be exact lowercase hex")
    root = dist_root(root_name)
    inner = f"""set -euo pipefail
umask 077
parent={STAGE_PARENT}
archive="$parent/candidate.tar.gz"
test ! -e "$parent"
test ! -L "$parent"
install -d -o root -g root -m 0700 /var/backups/celikpanel "$parent"
cat > "$archive"
printf '%s  %s\\n' {archive_sha256} "$archive" | sha256sum -c - >/dev/null
test "$(stat -Lc '%u:%g:%a:%h' "$archive")" = 0:0:600:1
tar --no-same-owner -xzf "$archive" -C "$parent"
test "$(cat {root}/release.commit)" = {commit}
test "$(cat {root}/release.tree)" = {tree}
test -x {root}/install.sh
test ! -e {root}/.git
stat -c 'STAGED=%n MODE=%a OWNER=%U:%G' /var/backups/celikpanel "$parent" "$archive" {root}
"""
    return "sudo -n /bin/bash -c " + shlex.quote(inner)


def credentials_document(username: str, email: str, password: str) -> bytes:
    return (json.dumps({"username": username, "email": email, "password": password},
                       separators=(",", ":")) + "\n").encode()


def upload_credentials() -> str:
    """Write stdin to the root:root 0600 credentials file install.sh consumes."""

    inner = f"""set -euo pipefail
umask 077
install -d -o root -g root -m 0700 {CREDENTIAL_DIR}
test ! -e {CREDENTIAL_FILE}
test ! -L {CREDENTIAL_FILE}
temporary=$(mktemp {CREDENTIAL_DIR}/.admin.XXXXXXXX)
trap 'rm -f -- "$temporary"' EXIT
cat > "$temporary"
size=$(stat -Lc %s -- "$temporary")
(( size >= 1 && size <= 4096 ))
chown root:root "$temporary"
chmod 0600 "$temporary"
mv -T -- "$temporary" {CREDENTIAL_FILE}
trap - EXIT
sync -f {CREDENTIAL_DIR}
stat -Lc 'CREDENTIALS=%n OWNER=%u:%g MODE=%a BYTES=%s' -- {CREDENTIAL_FILE}
"""
    return "sudo -n /bin/bash -c " + shlex.quote(inner)


def run_installer(root_name: str, *, skip_security_updates: bool = False) -> str:
    root = shlex.quote(dist_root(root_name))
    environment = f"SKIP_ADMIN=1 CELIKPANEL_ADMIN_CREDENTIALS_FILE={CREDENTIAL_FILE} "
    if skip_security_updates:
        environment += "SKIP_SECURITY_UPDATES=1 "
    return (
        "set -euo pipefail\n"
        "sudo -n env -u TRUSTED_RELEASE_ROOT -u CELIKPANEL_TRUSTED_RELEASE_ROOT "
        "-u http_proxy -u https_proxy -u ftp_proxy -u no_proxy "
        "-u HTTP_PROXY -u HTTPS_PROXY -u FTP_PROXY -u NO_PROXY -u ALL_PROXY -u all_proxy "
        f"{environment}/bin/bash -c 'cd -- \"$1\"; exec /bin/bash ./install.sh' celikpanel-direct {root}\n"
    )


def postinstall_check() -> str:
    return (
        "set -euo pipefail\n"
        "sudo -n test -f /etc/celikpanel/install.complete\n"
        f"sudo -n test ! -e {CREDENTIAL_FILE}\n"
        "systemctl is-active celikpanel-agent.service celikpanel-panel.service\n"
        "systemctl is-enabled celikpanel-agent.service celikpanel-panel.service\n"
    )


# ---------------------------------------------------------------------------
# The installer's closing restart notice (install.sh print_kernel_reboot_closing)
# ---------------------------------------------------------------------------

RESTART_REQUIRED_BANNER = "RESTART THIS SERVER NOW / BU SUNUCUYU SIMDI YENIDEN BASLATIN"
RESTART_RECOMMENDED_BANNER = "A RESTART IS RECOMMENDED / YENIDEN BASLATMA ONERILIR"
ANSI_RE = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")


def installer_restart_notice(stdout: str) -> dict[str, str]:
    """Which restart notice install.sh printed, and its exact text (ANSI colour removed).

    ``required``: the running kernel's modules are gone (nftables/WireGuard
    cannot load) and the owner must restart before using the panel.
    ``recommended``: a newer kernel is installed; nothing is broken.
    """

    lines = [ANSI_RE.sub("", line).rstrip() for line in stdout.splitlines()]
    for state, banner in (("required", RESTART_REQUIRED_BANNER), ("recommended", RESTART_RECOMMENDED_BANNER)):
        for index, line in enumerate(lines):
            if line.strip() != banner:
                continue
            block = [line.strip()]
            for following in lines[index + 1:index + 24]:
                if following and not following.startswith("    "):
                    break
                block.append(following)
            while block and not block[-1].strip():
                block.pop()
            return {"state": state, "banner": banner, "text": "\n".join(block)}
    return {"state": "none", "banner": "", "text": ""}


# ---------------------------------------------------------------------------
# Owner enrollment (cmd/dns-peer-enroll; --engine bind|pdns)
#
# cmd/dns-peer-enroll/README.md: every subcommand accepts --engine bind|pdns
# (default bind, so BIND command text is unchanged); a PowerDNS secondary needs
# --engine pdns on every command on both hosts, the packaged pdns-peer-inspect,
# and --catalog-account with the account stored for the catalog CONSUMER zone.
# ---------------------------------------------------------------------------

ENROLL_ENGINES = ("bind", "pdns")
INSPECTORS = {"bind": "bind-peer-inspect", "pdns": "pdns-peer-inspect"}
CATALOG_ACCOUNT_RE = re.compile(r"[a-z0-9_-]{1,128}")  # dnspeerenrollowner.validCatalogAccount


def _engine_arg(engine: str) -> str:
    if engine not in ENROLL_ENGINES:
        raise InstallError(f"unknown enrollment engine {engine}")
    return " --engine pdns" if engine == "pdns" else ""


def owner_tool(root_name: str, name: str) -> str:
    if name not in {"dns-peer-enroll", *INSPECTORS.values()}:
        raise InstallError(f"unknown owner tool {name}")
    return f"{dist_root(root_name)}/{OWNER_TOOLS}/{name}"


def enroll_primary_prepare(root_name: str, engine: str = "bind") -> str:
    return f"sudo -n {owner_tool(root_name, 'dns-peer-enroll')} primary-prepare{_engine_arg(engine)}"


def write_primary_public_key(path: str = "/root/celikpanel-primary-inspector.pub") -> str:
    inner = f"""set -euo pipefail
umask 077
test ! -e {path}
cat > {path}
chown root:root {path}
chmod 0600 {path}
"""
    return "sudo -n /bin/bash -c " + shlex.quote(inner)


def enroll_secondary_install(root_name: str, *, primary_ip: str, peer_ip: str, catalog: str,
                             public_key_path: str = "/root/celikpanel-primary-inspector.pub",
                             engine: str = "bind", catalog_account: str = "") -> str:
    extra = ""
    if engine == "pdns":
        if CATALOG_ACCOUNT_RE.fullmatch(catalog_account) is None:
            raise InstallError("a PowerDNS secondary needs the catalog CONSUMER zone's account")
        extra = f" --catalog-account {shlex.quote(catalog_account)}"
    elif catalog_account:
        raise InstallError("--catalog-account is PowerDNS only")
    return (
        f"sudo -n {owner_tool(root_name, 'dns-peer-enroll')} secondary-install{_engine_arg(engine)} "
        f"--primary-ip {shlex.quote(primary_ip)} --peer-ip {shlex.quote(peer_ip)} "
        f"--catalog {shlex.quote(catalog)}{extra} --primary-public-key {shlex.quote(public_key_path)} "
        f"--inspector {shlex.quote(owner_tool(root_name, INSPECTORS[engine]))}"
    )


def enroll_secondary_host_key(root_name: str, engine: str = "bind") -> str:
    return f"sudo -n {owner_tool(root_name, 'dns-peer-enroll')} secondary-host-key{_engine_arg(engine)}"


def enroll_primary_activate(root_name: str, *, credential_id: str, primary_ip: str, peer_ip: str,
                            catalog: str, host_key_sha256: str, engine: str = "bind") -> str:
    if not re.fullmatch(r"[0-9a-f]{16,64}", credential_id) or not HEX64.fullmatch(host_key_sha256):
        raise InstallError("credential ID or host-key digest is malformed")
    return (
        f"sudo -n {owner_tool(root_name, 'dns-peer-enroll')} primary-activate{_engine_arg(engine)} "
        f"--credential-id {credential_id} --primary-ip {shlex.quote(primary_ip)} "
        f"--peer-ip {shlex.quote(peer_ip)} --catalog {shlex.quote(catalog)} "
        f"--host-key-sha256 {host_key_sha256}"
    )


def enroll_status(root_name: str, side: str, engine: str = "bind") -> str:
    if side not in {"primary", "secondary"}:
        raise InstallError("status side must be primary or secondary")
    return f"sudo -n {owner_tool(root_name, 'dns-peer-enroll')} {side}-status{_engine_arg(engine)}"
