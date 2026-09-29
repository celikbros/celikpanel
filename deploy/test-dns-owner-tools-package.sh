#!/usr/bin/env bash
# Exercise the real dist recipe with actual DNS tools and inert unrelated payloads.
# Never invokes an installer, updater, enrollment or privileged command.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
for name in dns-peer-enroll bind-peer-inspect pdns-peer-inspect; do
    [[ -x "$root/bin/dns-owner-tools/$name" ]] || { echo 'Run make dns-owner-tools first' >&2; exit 1; }
done
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
cp -- "$root/Makefile" "$tmp/Makefile"
# The archive integration is real; core services are inert fixture files and
# deliberately do not establish whole-release or signed-candidate acceptance.
for file in bin/panel bin/agent bin/schema17-bridge bin/agent-native-contract.json \
    web/dist/index.html bin/firewall-runtime/restore \
    bin/firewall-runtime/celikpanel-firewall-restore.service \
    bin/mail-renewal-runtime/renew bin/mail-renewal-runtime/celikpanel-mail-host-cert \
    bin/recovery-runtime/bin/recovery bin/recovery-runtime/bin/agent-checker \
    bin/recovery-runtime/bin/panel-checker bin/recovery-runtime/bin/schema17-bridge \
    bin/recovery-runtime/update.sh bin/recovery-runtime/rollback.sh \
    bin/recovery-runtime/deploy/recovery/runtime-entry.sh \
    deploy/release-recovery-runner.sh deploy/release-recovery-foundation.sh \
    deploy/release-recovery.protocol deploy/systemd/celikpanel-release-recovery.service \
    deploy/systemd/celikpanel-release-recovery.timer download-portal/get.sh \
    install.sh bootstrap-update.sh bootstrap-prebuilt-update.sh update.sh rollback.sh \
    README.md SECURITY.md NOTICE; do
    mkdir -p -- "$tmp/$(dirname -- "$file")"
    printf 'inert packaging fixture\n' > "$tmp/$file"
done
cp -- "$root/deploy/write-release-manifest.sh" "$tmp/deploy/"
cp -r -- "$root/bin/dns-owner-tools" "$tmp/bin/dns-owner-tools"
version=v0.0.0-owner-tools-test
archive="$tmp/dist/celikpanel-$version.tar.gz"
first=''
for mask in 022 077; do
    ( umask "$mask"; make --no-print-directory -C "$tmp" -o build dist \
        VERSION="$version" COMMIT=0123456789abcdef0123456789abcdef01234567 \
        TREE=89abcdef0123456789abcdef0123456789abcdef SOURCE_DATE_EPOCH=0 ) > "$tmp/make.log" 2>&1 \
        || { cat "$tmp/make.log"; exit 1; }
    current=$(sha256sum "$archive" | cut -d ' ' -f1)
    [[ -z "$first" || "$first" == "$current" ]] || { echo 'umask changed archive' >&2; exit 1; }
    first=$current
done
mkdir "$tmp/unpacked"
tar -xzf "$archive" -C "$tmp/unpacked"
release="$tmp/unpacked/celikpanel-$version"
(cd "$release" && sha256sum -c SHA256SUMS >/dev/null)
for name in dns-peer-enroll bind-peer-inspect pdns-peer-inspect; do
    cmp -- "$root/bin/dns-owner-tools/$name" "$release/dns-owner-tools/$name"
    [[ "$(stat -c %a "$release/dns-owner-tools/$name")" == 755 ]]
    grep -Fq " ./dns-owner-tools/$name" "$release/SHA256SUMS"
done
cmp -- "$root/cmd/dns-peer-enroll/README.md" "$release/dns-owner-tools/README.md"
[[ "$(stat -c %a "$release/dns-owner-tools/README.md")" == 644 ]]
# No-argument invocation only prints usage; it must not start enrollment.
if "$release/dns-owner-tools/dns-peer-enroll" > "$tmp/usage" 2>&1; then
    echo 'missing action unexpectedly accepted' >&2; exit 1
fi
grep -Fq 'usage: dns-peer-enroll' "$tmp/usage"
printf '\ntamper\n' >> "$release/dns-owner-tools/bind-peer-inspect"
if (cd "$release" && sha256sum -c SHA256SUMS >/dev/null 2>&1); then
    echo 'modified inspector passed release checksum' >&2; exit 1
fi
printf '%s\n' 'DNS owner tools: real dist payload, executable modes, umask reproducibility and tamper detection passed'
