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

## Operator gate, CI, and portal assembly

No production private key is generated or stored in this repository. The
canonical Ed25519 public key is intentionally tracked at
`deploy/release-signing-ed25519.pem` and is the public trust root pinned by
`download-portal/get.sh`. Every tagged release requires the
`CELIKPANEL_RELEASE_SIGNING_ED25519_PEM` GitHub Actions secret and the
monotonic `CELIKPANEL_RELEASE_SEQUENCE` repository variable. The signer derives
the public key from that private key and requires exact byte equality with the
tracked PEM. A missing key, sequence, or key mismatch fails the tag job closed.
Successful tag CI publishes exactly six assets: generic archive/checksum,
linux/amd64 archive/checksum, and the detached signed manifest/signature.

The preferred portal publisher does not receive a private key. Download the
six immutable assets from the completed tag job, verify that there are no
additional or missing files, verify both checksum files, and prove that the
generic and platform archives are byte-identical:

    (cd CI_ASSET_DIRECTORY && \
      sha256sum -c celikpanel-VERSION.tar.gz.sha256 && \
      sha256sum -c celikpanel-VERSION-linux-amd64.tar.gz.sha256 && \
      cmp -s celikpanel-VERSION.tar.gz \
        celikpanel-VERSION-linux-amd64.tar.gz)

Then assemble a new, non-existing portal candidate from the four authoritative
platform assets:

    CELIKPANEL_RELEASE_SIGNING=pre-signed \
    CELIKPANEL_RELEASE_SEQUENCE=41 \
    CELIKPANEL_RELEASE_TREE=EXACT_40_HEX_TAG_TREE \
    CELIKPANEL_RELEASE_OS=linux \
    CELIKPANEL_RELEASE_ARCH=amd64 \
    CELIKPANEL_RELEASE_SIGNED_MANIFEST_FILE=CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.release-manifest-v2 \
    CELIKPANEL_RELEASE_SIGNED_SIGNATURE_FILE=CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.release-manifest-v2.sig \
      deploy/build-download-portal.sh VERSION COMMIT PUBLISHED_AT \
        CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.tar.gz \
        CI_ASSET_DIRECTORY/celikpanel-VERSION-linux-amd64.tar.gz.sha256 \
        NEW_PORTAL_CANDIDATE

`pre-signed` mode rejects a private-key environment variable. It pins each
input to an opened descriptor, requires the official filenames, an exact
canonical checksum line, an exact ten-line manifest matching every release
argument, and a raw 64-byte signature verified by the tracked PEM. It also
requires exact LF-terminated `release.version`, `release.commit`, and
`release.tree` members in the signed archive. The platform archive,
checksum, manifest, and signature are copied byte-for-byte and reverified after
staging. The generic compatibility archive is recreated from that exact
platform archive; it is never update authority.

The private-key `required` mode remains available for an isolated signing
runner, not the production portal host:

    CELIKPANEL_RELEASE_SIGNING=required \
    CELIKPANEL_RELEASE_SIGNING_KEY_FILE=/run/secrets/release-ed25519.pem \
    CELIKPANEL_RELEASE_SEQUENCE=42 \
    CELIKPANEL_RELEASE_OS=linux \
    CELIKPANEL_RELEASE_ARCH=amd64 \
      deploy/build-download-portal.sh VERSION COMMIT PUBLISHED_AT \
        PLATFORM_ARCHIVE PLATFORM_CHECKSUM OUTPUT_DIR

### Release archive bootstrap verification

Before signing or publishing, verify that the reproducible `make dist` output
contains the reviewed first-install code and trust root without transformations:

    root=celikpanel-VERSION
    tar -xOzf PLATFORM_ARCHIVE "$root/libexec/get.sh" \
      | cmp -s - download-portal/get.sh
    tar -xOzf PLATFORM_ARCHIVE "$root/install.sh" \
      | cmp -s - install.sh
    tar -xOzf PLATFORM_ARCHIVE "$root/deploy/release-signing-ed25519.pem" \
      | cmp -s - deploy/release-signing-ed25519.pem

Also require exact, single regular archive members for those three paths and
for `release.version`, `release.commit`, and `release.tree`. The
archive must be the exact archive whose digest and size occur in the verified
signed manifest. Portal publication remains a separate same-filesystem atomic
exchange; a candidate is never merged into the live tree, and every previous
version and rollback backup is preserved.

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

The in-panel worker and update API never choose initial trust. A fresh
installation instead enters through the public, release-pinned
`download-portal/get.sh`: it accepts only its embedded version and sequence,
downloads the portal-root PEM, verifies its embedded SHA-256 trust-anchor
digest, verifies the detached manifest, archive digest/size, internal
checksums, and release commit, then passes that exact authenticated identity to
`install.sh` while holding the persistent update lock. The installer
preflights and enrolls the same public key and sequence floor only after the
installed panel/agent identity is proven. `latest` never authorizes this flow.

If a first installation is interrupted after trust enrollment, retry only
with the same release-pinned bootstrap and explicit `--install`. Once the live
portal advances to a later release, its current `get.sh` must not be used to
recover the older floor. Use the already installed
`/usr/libexec/celikpanel/get.sh` when present, or extract `libexec/get.sh` from
the exact older immutable release asset after verifying that asset. Never edit
or lower `sequence.floor` to force recovery.

After enrollment, the worker requires the pre-existing
`/var/lib/celikpanel-release-state/sequence.floor` and passes its sequence as
`--minimum-sequence`. For a normal upgrade this current floor is strictly
lower than the exact expected target sequence; an exact
same-sequence/same-version retry is the only equality case.

### One-time enrollment on existing servers

Fresh Alpha41 installations use the signed bootstrap flow above and do not
need a separate manual enrollment. For an existing installation, first
install or upgrade the reviewed panel/agent pair through the normal paired
release procedure and verify both binaries report the intended build.
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
