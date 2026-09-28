#!/usr/bin/env python3
"""Check the disposable PowerDNS SSH forced command before DNS mutation."""

import argparse
import json
from pathlib import Path
import subprocess

import guest_bootstrap as bootstrap
import native_pdns_inspector_channel as peer


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--identity-file", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).with_name("manifest.json"))
    args = parser.parse_args()
    primary, _, identity = peer.selected(args)
    local = peer.local_key_dir(args)
    key, known = local / "id_ed25519", local / "known_hosts"
    if not key.is_file() or not known.is_file():
        parser.error("first enroll the isolated PowerDNS inspector")
    peer.transfer(primary, identity, key, peer.STAGE + ".transport-key")
    peer.transfer(primary, identity, known, peer.STAGE + ".transport-known")
    root = "/etc/celikpanel-native-pdns-peer-proof"
    peer.remote(primary, identity, " && ".join((
        f"sudo install -d -m 0700 -o root -g root {root}",
        f"sudo install -m 0600 -o root -g root {peer.STAGE}.transport-key {root}/id_ed25519",
        f"sudo install -m 0600 -o root -g root {peer.STAGE}.transport-known {root}/known_hosts",
        f"rm -- {peer.STAGE}.transport-key {peer.STAGE}.transport-known",
    )))
    base = (f"sudo ssh -F /dev/null -i {root}/id_ed25519 -o IdentitiesOnly=yes"
            " -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=yes"
            f" -o UserKnownHostsFile={root}/known_hosts"
            " -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no"
            " -o ClearAllForwardings=yes -T celikpeer@192.0.2.11")
    attempted = subprocess.run(bootstrap.ssh_base(primary, identity) +
                               [base + " celikpanel-pdns-peer-inspect-v1-unexpected"],
                               check=False, capture_output=True, text=True, timeout=20)
    if attempted.returncode != 126 or attempted.stdout:
        parser.error("PowerDNS peer forced command admitted an altered SSH command")
    untrusted = subprocess.run(bootstrap.ssh_base(primary, identity) +
                               [base.replace(f"UserKnownHostsFile={root}/known_hosts",
                                             "UserKnownHostsFile=/dev/null") +
                                " celikpanel-pdns-peer-inspect-v1-unexpected"],
                               check=False, capture_output=True, text=True, timeout=20)
    if untrusted.returncode != 255 or untrusted.stdout or "Host key verification failed" not in untrusted.stderr:
        parser.error("PowerDNS peer accepted an untrusted SSH host key")
    print(json.dumps({"altered_original_command_denied": True,
                      "untrusted_host_denied": True,
                      "peer": peer.PEER_IP, "management_installed_on_peer": False}, sort_keys=True))


if __name__ == "__main__":
    main()
