#!/usr/bin/env python3
"""Exercise signed receipt replacement in isolated root-owned directories.

Production policy functions run in bash and dash. Only host paths and account /
systemd discovery are isolated; signatures, inode checks, staging and moves are real.
"""
import os
from pathlib import Path
import subprocess
import tempfile

assert os.geteuid() == 0, 'run in a disposable root test environment'
repo = Path(__file__).resolve().parent.parent
source = (repo / 'download-portal/get.sh').read_text()
policy = source[source.index('validate_root_directory_chain()'):source.index('# BEGIN DOWNLOAD OPERATION POLICY')]

with tempfile.TemporaryDirectory(prefix='celikpanel-unstarted-', dir='/var/lib') as directory:
    root = Path(directory)
    # Keep file and trust validation real, but never inspect or mutate host state.
    isolated = policy.replace('/opt/celikpanel', str(root / 'host/opt/celikpanel'))
    for prefix in ['/etc/celikpanel', '/var/lib/celikpanel', '/run/celikpanel',
                   '/usr/libexec/celikpanel', '/etc/letsencrypt/', '/etc/systemd/',
                   '/run/systemd/', '/usr/lib/systemd/', '/lib/systemd/']:
        isolated = isolated.replace(prefix, str(root / 'host') + prefix)
    (root / 'policy.sh').write_text(isolated)
    subprocess.run(['openssl', 'genpkey', '-algorithm', 'ED25519', '-out', str(root / 'key')], check=True)
    subprocess.run(['openssl', 'pkey', '-in', str(root / 'key'), '-pubout', '-out', str(root / 'public')], check=True)
    probe = r'''set -eu
umask 077
test_root=$1
scenario=$2
. "$test_root/policy.sh"
message() { printf '%s\n' "$1"; }
fail() { message "$1" >&2; exit 1; }
getent() { return "${account_result:-2}"; }
systemctl() { printf '%s\n' "${unit_state:-not-found}"; }
workdir=$test_root/work
state=$test_root/state
mkdir -m 0700 "$workdir" "$state"
release_sequence_floor=$state/sequence.floor
release_public_key=$test_root/host/etc/celikpanel/release-signing-ed25519.pem
signed_update_lock=$state/update.lock
pending_install_directory=$state/install.pending
pending_install_identity=
replace_pending_metadata=
resume_first_install=0
legacy_first_install=0
bootstrap_release_sequence=61
bootstrap_release_public_key_sha256=$(sha256sum "$test_root/public" | awk '{print $1}')
signed_commit=0123456789abcdef0123456789abcdef01234567
signed_archive_sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
signed_archive_size=1234
runtime_release_identity
signed_public_key_path=$workdir/release-signing-ed25519.pem
install -m 0600 "$test_root/public" "$signed_public_key_path"
manifest() {
  signed_release_sequence=$1
  version=v0.1.0-alpha.$1
  printf '%s\n' format=celikpanel-release-manifest-v2 "sequence=$1" "version=$version" \
    "commit=$signed_commit" published_at=2026-09-10T00:00:00Z os=linux "arch=$runtime_release_arch" \
    "archive=celikpanel-$version-linux-$runtime_release_arch.tar.gz" \
    "archive_sha256=$signed_archive_sha256" "archive_size=$signed_archive_size" > "$workdir/release-manifest-v2"
  openssl pkeyutl -sign -rawin -inkey "$test_root/key" -in "$workdir/release-manifest-v2" -out "$workdir/release-manifest-v2.sig"
}
manifest 59
provision_first_install_signed_update_lock
acquire_signed_update_lock
publish_pending_first_install
original=$(inspect_pending_first_install "$pending_install_directory")
replace_pending_metadata=$original
manifest 61
case "$scenario" in
  bad-signature) printf x >> "$workdir/release-manifest-v2.sig" ;;
  changed-receipt)
    mv "$pending_install_directory" "$state/saved"
    cp -a "$state/saved" "$pending_install_directory"
    original=$(inspect_pending_first_install "$pending_install_directory") ;;
  existing-data|dangling-data)
    mkdir -p "$test_root/host/var/lib"
    if [ "$scenario" = existing-data ]; then mkdir "$test_root/host/var/lib/celikpanel"; else
      ln -s "$test_root/absent" "$test_root/host/var/lib/celikpanel"
    fi ;;
  existing-unit)
    mkdir -p "$test_root/host/etc/systemd/system"
    touch "$test_root/host/etc/systemd/system/celikpanel-agent.service" ;;
  existing-account) account_result=0 ;;
  discovery-error) account_result=1 ;;
  loaded-unit) unit_state=loaded ;;
  interrupted)
    mv() {
      case "$*" in *'.install-pending.'*) return 1 ;; esac
      command mv "$@"
    } ;;
  success) ;;
  *) exit 2 ;;
esac
if [ "$scenario" = success ]; then
  publish_pending_first_install
  current=$(inspect_pending_first_install "$pending_install_directory")
  case "$current" in '61 v0.1.0-alpha.61 '*) ;; *) exit 1 ;; esac
elif [ "$scenario" = interrupted ]; then
  if publish_pending_first_install; then exit 1; fi
  [ ! -e "$pending_install_directory" ]
  unset -f mv
  replace_pending_metadata=
  publish_pending_first_install
  case "$(inspect_pending_first_install "$pending_install_directory")" in '61 v0.1.0-alpha.61 '*) ;; *) exit 1 ;; esac
else
  if publish_pending_first_install; then echo "Unsafe replacement: $scenario" >&2; exit 1; fi
  [ "$(inspect_pending_first_install "$pending_install_directory")" = "$original" ]
fi
case "$scenario" in success|interrupted)
  set -- "$state"/.install-superseded.*/record
  [ "$#" -eq 1 ]
  [ "$(inspect_pending_first_install "$1")" = "$original" ] ;;
esac
'''
    (root / 'probe.sh').write_text(probe)
    import shutil
    for shell in ['bash', 'dash']:
        for scenario in ['success', 'bad-signature', 'changed-receipt', 'existing-data',
                         'dangling-data', 'existing-unit', 'existing-account',
                         'discovery-error', 'loaded-unit', 'interrupted']:
            subprocess.run([shell, str(root / 'probe.sh'), str(root), scenario], check=True, timeout=30)
            print(f'PASS {shell}: {scenario}', flush=True)
            for child in ['work', 'state', 'host']:
                if (root / child).exists(): shutil.rmtree(root / child)
