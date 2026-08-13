# Signed release and rollback-floor contract

The automated update trust root is operator-owned. `celikpanel.net`, its JSON,
`latest.txt`, HTTPS, and an adjacent checksum are distribution aids; none of
them may select or authorize a privileged update.

## Canonical signed manifest v2

`release-manifest-v2.sig` is a raw detached Ed25519 signature over exactly ten
ASCII, LF-terminated lines, in this order:

    format=celikpanel-release-manifest-v2
    sequence=42
    version=v1.2.3-alpha.4
    commit=40-lowercase-hex-characters
    published_at=YYYY-MM-DDTHH:MM:SSZ
    os=linux
    arch=amd64
    archive=celikpanel-v1.2.3-alpha.4-linux-amd64.tar.gz
    archive_sha256=64-lowercase-hex-characters
    archive_size=positive-canonical-decimal

`sequence` is an operator-assigned, monotonically increasing integer in
`1..9223372036854775807`; it is transported as text, never a JavaScript number.
`published_at` is informational and is never an ordering authority. Archive
size is limited to 2 GiB. Version is canonical SemVer without build metadata;
numeric prerelease identifiers cannot have leading zeroes.

Signed objects use immutable, platform-separated paths:

    /releases/VERSION/OS/ARCH/release-manifest-v2
    /releases/VERSION/OS/ARCH/release-manifest-v2.sig
    /releases/VERSION/OS/ARCH/celikpanel-VERSION-OS-ARCH.tar.gz
    /releases/VERSION/OS/ARCH/celikpanel-VERSION-OS-ARCH.tar.gz.sha256

The signer checks that both `bin/panel` and `bin/agent` are bounded regular
ELF64 members for the claimed architecture and that `go version -m` reports the
exact signed `GOOS`/`GOARCH` pair. A Go tool capable of reading build metadata
is therefore required on the isolated signing runner. It re-hashes the archive immediately
before publication. The portal builder also proves source/staged byte equality
and re-hashes the staged copy before signing. Legacy unsigned generic paths and
the six-argument portal build remain unchanged.

## Operator gate and CI

No production private or public key is generated or stored in this repository.
Tag CI creates signed linux/amd64 assets only when the operator configures the
`CELIKPANEL_RELEASE_SIGNING_ED25519_PEM` GitHub Actions secret. When that secret
is present, repository variable `CELIKPANEL_RELEASE_SEQUENCE` is mandatory and
missing or invalid configuration fails closed. When the secret is absent, the
existing unsigned release artifacts are still produced and published.

Equivalent portal staging is explicit:

    CELIKPANEL_RELEASE_SIGNING=required \
    CELIKPANEL_RELEASE_SIGNING_KEY_FILE=/run/secrets/release-ed25519.pem \
    CELIKPANEL_RELEASE_SEQUENCE=42 \
    CELIKPANEL_RELEASE_OS=linux \
    CELIKPANEL_RELEASE_ARCH=amd64 \
      deploy/build-download-portal.sh VERSION COMMIT PUBLISHED_AT \
        PLATFORM_ARCHIVE PLATFORM_CHECKSUM OUTPUT_DIR

## Installed trust material and anti-rollback floor

Every release packages the reviewed updater as `libexec/get.sh`; installation
publishes those exact bytes atomically at `/usr/libexec/celikpanel/get.sh`.
The privileged worker must execute that installed copy, never a script fetched
from the portal. An Ed25519 public key is provisioned only when the operator
passes `CELIKPANEL_RELEASE_PUBLIC_KEY_FILE=/exact/path.pem` to installation.
Its exact canonical bytes are atomically installed at
`/etc/celikpanel/release-signing-ed25519.pem`; an absent input never creates,
replaces, or invents a key.

The trusted invocation is exact and never uses `latest`:

    /usr/libexec/celikpanel/get.sh --update --version VERSION \
      --require-signed-manifest --expected-sequence SEQUENCE \
      --minimum-sequence CURRENT_FLOOR \
      --expected-commit COMMIT \
      --expected-archive-sha256 ARCHIVE_SHA256 \
      --expected-archive-size ARCHIVE_SIZE

The worker never performs first trust. Before the update API is enabled, an
operator-controlled enrollment step must provision both the pinned public key
and a floor for the currently trusted installed release. The portal, `latest`
metadata, and an update candidate cannot supply that initial floor. The worker
requires the pre-existing `/var/lib/celikpanel-release-state/sequence.floor`
and passes its sequence as `--minimum-sequence`. For a normal upgrade this
current floor is strictly lower than the exact expected target sequence; an
exact same-sequence/same-version retry is the only equality case.

### One-time enrollment on existing and new servers

First install or upgrade the reviewed panel/agent pair through the normal
paired release procedure and verify both binaries report the intended build.
An older release that does not expose the read-only
`--inspect-build-identity` mode cannot be enrolled: deploy this feature as a
manually approved paired release first, then enroll that installed release.
The first signed in-panel update must use a strictly greater sequence.

Run the helper only from the authenticated checkout or verified release tree
whose code was reviewed by the operator. It never contacts the portal, never
restarts a service, and never accepts `latest` as authority:

    sudo bash deploy/enroll-signed-release-trust.sh \
      --sequence CURRENT_TRUSTED_SEQUENCE \
      --version CURRENT_INSTALLED_VERSION \
      --commit CURRENT_INSTALLED_40_HEX_COMMIT \
      --public-key-file /canonical/root-owned/release-ed25519-public.pem

The public-key source must be canonical, root:root, single-link, and not
group/other writable. The helper executes the fixed installed
`/opt/celikpanel/bin/panel` and `/opt/celikpanel/bin/agent` probes before and
after taking the persistent update lock. Both version/commit identities must
match each other and every explicit operator argument. Only then does it
atomically publish the exact key and three-line floor, fsyncing files and
parent directories. An exact retry is idempotent. A different key, lower or
higher sequence, same sequence with another version, mismatched binary pair,
unsafe metadata, or concurrent update is refused without replacing trust.

Enrollment does not make the current release signed retroactively. It records
the operator-approved current floor so the next signed release can advance it.
Keep the private Ed25519 key outside every server; only the public key is
enrolled. Run the same explicit procedure separately on each server after its
paired build identity has been verified.

The floor is a root:root mode-0600 regular single-link file beneath a
root:root mode-0700 directory. Its exact three-line format is:

    format=celikpanel-release-sequence-floor-v1
    sequence=42
    version=v1.2.3-alpha.4

The effective minimum is the maximum of the worker-supplied current floor and
the independently re-read durable floor.
A lower sequence is rejected. The same sequence is accepted only for the same
version, allowing safe retry; the same sequence with another version is
rejected. A root-owned nonblocking flock serializes the complete signed path so
concurrent trusted invocations cannot publish floors out of order. After
signature, platform, size, digest, internal checksum, and
`release.commit` checks all pass—but before any host mutation—the updater
atomically advances and fsyncs the floor. Signed downloads reject redirects.

The installer atomically pre-provisions the persistent
`/var/lib/celikpanel-release-state/update.lock` as a root:root mode-0600,
zero-byte, single-link file beneath the exact root:root mode-0700 state
directory. The updater only opens and nonblocking-flocks that pre-existing
inode; it never creates or replaces the lock. The descriptor remains inherited
through bootstrap and apply-only installation, while the installer only
revalidates the same path and therefore does not recursively flock and
self-deadlock.

The Linux agent RPC and the admin-only panel API/UI use this contract. Check
and start fail closed when the pinned key or canonical pre-existing floor is
absent, unsafe, or inconsistent. The UI never supplies a URL, filesystem path,
command, sequence, commit, digest, or size; it can only confirm and submit the
exact signed identity returned by the trusted check path.

Snapshot schema v6 includes the installed reviewed updater and its
present/absent state. The apply-only installer publishes the exact
trusted-release copy only after `update.sh` has durably captured those prior
bytes, and update completion proves the installed copy. Rollback restores or
removes it from the verified snapshot and proves its metadata and bytes,
preventing an old agent/new updater mixed-version state.
