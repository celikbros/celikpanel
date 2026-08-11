# AlmaLinux/Rocky Linux 9 blocked smoke probe

This directory provides one narrow check for a quiescent, disposable x86_64
AlmaLinux 9 or Rocky Linux 9 VM. It proves only that the reviewed preview
installer returns the exact refusal and that the enumerated durable checkpoints
have equal before/after digests.

It is not a RHEL-family certification harness. It implements no successful
install, HTTP default-deny, real DNF/RPM-lock, reboot, update, rollback, or
AVC-free certification workflow. A regressed root installer could still attempt
such an action; the probe fails on any enumerated persistent difference and
does not claim to observe every transient effect. Those independent journeys
remain mandatory before RHEL-family activation.

## Fixed remote layout and identity

The controller accepts no remote path. Stage the candidate below:

    /root/celikpanel-e2e/DISPOSABLE_TARGET
    /root/celikpanel-e2e/release/SHA256SUMS
    /root/celikpanel-e2e/release/install.sh

DISPOSABLE_TARGET must be root:root, canonical, non-symlink, not group/other
writable, have one hard link, and contain exactly:

    CELIKPANEL_RHEL9_DISPOSABLE_V1
    distro=almalinux
    machine_id=0123456789abcdef0123456789abcdef
    nonce=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef

Use distro=rocky on Rocky Linux. The machine ID must equal /etc/machine-id.
Generate the nonce out of band and pin both values locally.

SHA256SUMS uses: 64 lowercase hex characters, two spaces, then a relative
path. Every release file except SHA256SUMS itself must occur exactly once.
Paths may contain only letters, digits, dot, underscore, slash, at, plus and
minus. The probe rejects symlinks, special files, hard links, nested mounts,
unlisted files, non-root ownership, and writable ancestors or objects. Review
the manifest and pin its own SHA-256 digest out of band.

Use a dedicated known_hosts file with exactly one plain ssh-ed25519 line for
CELIKPANEL_E2E_HOST_KEY_ALIAS. Wildcards, comma aliases, certificates, hashed
names, comments and extra keys are refused. The file and all ancestors must be
canonical, root/invoking-user owned, and not group/other writable. Pin its
SHA256 fingerprint separately. An identity file must be invoking-user owned
with mode 0400 or 0600.

SSH disables global known-host files, DNS host-key verification, host-key
updates, forwarding, proxy commands, passwords and local commands. Remote
commands are four literal phase/distro variants; environment values are never
quoted into a remote command. SSH-consumed file paths use a token-free grammar,
so OpenSSH cannot expand percent-h-style tokens or split a known-host path list.
Each connection receives the pinned machine ID, target nonce and manifest
digest as a strict hexadecimal preamble; the remote probe compares all three
before it can invoke the installer.

Run the local controller from a canonical root/invoking-user-owned directory
chain that is not group/other writable. The streamed remote runner is subject
to the same local file, hard-link and ancestor checks as known_hosts. The local
OS/SSH/timeout stack and the remote kernel, sshd, sudo, env, bash, readlink and
stat are bootstrap trust roots: a shell probe cannot attest them before they
execute.
Once running, the probe verifies the fixed evidence-tool paths as canonical,
root:root, single-linked and not group/other writable before collecting state.

## Dry-run example

    CELIKPANEL_E2E_DRY_RUN=1 \
    CELIKPANEL_E2E_SSH_TARGET=root@alma9.example \
    CELIKPANEL_E2E_EXPECT_ID=almalinux \
    CELIKPANEL_E2E_KNOWN_HOSTS=/secure/alma9.known_hosts \
    CELIKPANEL_E2E_HOST_KEY_ALIAS=celikpanel-alma9-e2e \
    CELIKPANEL_E2E_EXPECT_HOST_KEY_SHA256=SHA256:REPLACE_WITH_43_BASE64_CHARS \
    CELIKPANEL_E2E_EXPECT_MANIFEST_SHA256=REPLACE_WITH_64_LOWERCASE_HEX \
    CELIKPANEL_E2E_EXPECT_MACHINE_ID=REPLACE_WITH_32_LOWERCASE_HEX \
    CELIKPANEL_E2E_EXPECT_TARGET_NONCE=REPLACE_WITH_64_LOWERCASE_HEX \
    deploy/e2e/rhel9/run.sh

Dry-run is the default even when the variable is omitted. For an explicitly
authorized disposable target, set `CELIKPANEL_E2E_DRY_RUN=0` and also set
`CELIKPANEL_E2E_ALLOW_REMOTE_BLOCKED_NONCE` to the exact pinned target nonce.
Changing the dry-run flag alone cannot start SSH.
The non-installer inspect call first binds host key, distro, machine ID, nonce and
manifest. The blocked call then invokes install.sh without SKIP_DEPS or
SKIP_ADMIN, repeats those bindings before execution, and requires its exact
preview refusal. The installer is limited to 120 seconds and 64 KiB of captured
output; each complete SSH phase is limited to 300 seconds. Combined remote
stdout/stderr is capped locally at 4 KiB and success requires an exact, closed
RESULT-line grammar. No raw remote or installer output is replayed to the
terminal; only verified fields and digests are reported.

The durable digest observes the enumerated product/transaction/backup/unit paths,
RPM inventory and databases, DNF state/cache/logs, account databases, systemd
unit files and current states, firewall configuration and normalized rulesets,
boot ID, SELinux enforcing state, loaded policy, and local SELinux policy
stores. It also requires no DNF/RPM/CelikPanel process at either checkpoint.

Each phase takes a nonblocking lock on the fixed target root. Evidence is built
in a root-owned mode-0700 scratch directory under /run, then that scratch is
removed; the scratch itself is a deliberate transient probe artifact and is not
part of the durable equality claim. File enumeration and sorting are separately
status-checked, mountinfo is pinned to the probe process, and every mountpoint
at or below an observed tree is rejected, including same-filesystem bind mounts.
The target must otherwise be quiescent: unrelated package, service, firewall or
account administration can correctly make the before/after comparison fail.

Equal snapshots are not proof that no transient syscall or mutation occurred
between them. Basic file metadata, contents and SELinux contexts are measured;
POSIX ACLs, file capabilities and non-SELinux extended attributes are not a
complete part of this shell-level proof. A malicious same-root installer could
also subvert the measurement toolchain, which is outside this smoke test's
threat model. The result is named durable-checkpoints=unchanged and must never
be presented as certification or a complete pre-mutation proof.
