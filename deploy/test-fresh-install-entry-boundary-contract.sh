#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
installer=$repo_root/install.sh
bootstrap=$repo_root/download-portal/get.sh
guard=$repo_root/deploy/release-transaction-guard.sh

fail() {
    printf 'fresh-install entry-boundary contract failed: %s\n' "$1" >&2
    exit 1
}

[[ $(id -u) -eq 0 ]] || fail "run this test as root"
for required in awk bash cp curl dirname env find grep id mktemp openssl \
    python3 readlink script sed sha256sum sort stat tar uname xargs; do
    command -v "$required" >/dev/null 2>&1 || fail "$required is required"
done

tmp=$(mktemp -d /var/lib/celikpanel-fresh-entry-contract.XXXXXXXX)
cleanup() {
    case "$tmp" in
        /var/lib/celikpanel-fresh-entry-contract.*) ;;
        *) fail "refusing to clean unexpected fixture path: $tmp" ;;
    esac
    [[ -d $tmp && ! -L $tmp && $(readlink -e -- "$tmp") == "$tmp" ]] \
        || fail "fixture root changed during cleanup"
    rm -rf -- "$tmp"
}
trap cleanup EXIT HUP INT TERM
chmod 0700 "$tmp"
chown root:root "$tmp"

version=$(sed -n 's/^bootstrap_release_version=//p' "$bootstrap")
sequence=$(sed -n 's/^bootstrap_release_sequence=//p' "$bootstrap")
[[ -n $version && $(grep -c '^bootstrap_release_version=' "$bootstrap") -eq 1 ]] \
    || fail "bootstrap version pin is ambiguous"
[[ $sequence =~ ^[1-9][0-9]*$ && $(grep -c '^bootstrap_release_sequence=' "$bootstrap") -eq 1 ]] \
    || fail "bootstrap sequence pin is ambiguous"
case "$(uname -m)" in
    x86_64) release_arch=amd64 ;;
    aarch64) release_arch=arm64 ;;
    *) fail "unsupported contract-test architecture: $(uname -m)" ;;
esac

commit=0123456789abcdef0123456789abcdef01234567
tree=89abcdef0123456789abcdef0123456789abcdef
release_name=celikpanel-$version
release_root=$tmp/source/$release_name
mkdir -p "$release_root/deploy"
cp -- "$installer" "$release_root/install.sh"
cp -- "$guard" "$release_root/deploy/release-transaction-guard.sh"

# This test-only second source is reached only after the real installer has
# sourced the real guard. Returning 86 makes set -e terminate before any host
# mutation while proving which authority supplied the root.
cat > "$release_root/deploy/release-recovery-foundation.sh" <<'EOF'
case "${CELIKPANEL_ENTRY_BOUNDARY_EXPECT_MODE:-}" in
    direct)
        [[ "$FIRST_INSTALL_TRUST_REQUESTED" == 0 ]]
        [[ -z "${CELIKPANEL_TRUSTED_RELEASE_ROOT+x}" ]]
        [[ "$TRUSTED_RELEASE_ROOT" == "$SRC" ]]
        [[ "$SRC" == "$CELIKPANEL_ENTRY_BOUNDARY_EXPECT_ROOT" ]]
        ;;
    signed)
        [[ "$FIRST_INSTALL_TRUST_REQUESTED" == 1 ]]
        [[ "$CELIKPANEL_TRUSTED_RELEASE_ROOT" == "$SRC" ]]
        [[ "$TRUSTED_RELEASE_ROOT" == "$SRC" ]]
        [[ "$SRC" == "$CELIKPANEL_ENTRY_BOUNDARY_EXPECT_PREFIX/"* ]]
        ;;
    *) return 85 ;;
esac
[[ "$SRC" == /* && "$(readlink -e -- "$SRC")" == "$SRC" ]]
printf 'S3_ENTRY_BOUNDARY_REACHED mode=%s root=%s\n' \
    "$CELIKPANEL_ENTRY_BOUNDARY_EXPECT_MODE" "$SRC"
return 86
EOF

printf '1\n' > "$release_root/release.version"
printf '%s\n' "$commit" > "$release_root/release.commit"
printf '%s\n' "$tree" > "$release_root/release.tree"
chmod 0755 "$release_root/install.sh"
chmod 0644 "$release_root/deploy/release-transaction-guard.sh" \
    "$release_root/deploy/release-recovery-foundation.sh" \
    "$release_root/release.version" "$release_root/release.commit" \
    "$release_root/release.tree"
(
    cd "$release_root"
    LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 sha256sum > SHA256SUMS
)

archive_name=$release_name-linux-$release_arch.tar.gz
archive=$tmp/$archive_name
tar -czf "$archive" -C "$tmp/source" "$release_name"
archive_sha=$(sha256sum "$archive" | awk '{print $1}')
archive_size=$(stat -Lc '%s' -- "$archive")

openssl genpkey -algorithm ED25519 -out "$tmp/private.pem" >/dev/null 2>&1
chmod 0600 "$tmp/private.pem"
openssl pkey -in "$tmp/private.pem" -pubout -out "$tmp/public.pem" \
    >/dev/null 2>&1
public_sha=$(sha256sum "$tmp/public.pem" | awk '{print $1}')

site=$tmp/site
release_site=$site/releases/$version/linux/$release_arch
mkdir -p "$release_site"
cp -- "$tmp/public.pem" "$site/release-signing-ed25519.pem"
cp -- "$archive" "$release_site/$archive_name"
printf '%s  %s\n' "$archive_sha" "$archive_name" \
    > "$release_site/$archive_name.sha256"
cat > "$release_site/release-manifest-v2" <<EOF
format=celikpanel-release-manifest-v2
sequence=$sequence
version=$version
commit=$commit
published_at=2026-08-31T00:00:00Z
os=linux
arch=$release_arch
archive=$archive_name
archive_sha256=$archive_sha
archive_size=$archive_size
EOF
openssl pkeyutl -sign -rawin -inkey "$tmp/private.pem" -passin pass: \
    -in "$release_site/release-manifest-v2" \
    -out "$release_site/release-manifest-v2.sig"
[[ $(stat -Lc '%s' -- "$release_site/release-manifest-v2.sig") -eq 64 ]] \
    || fail "test manifest signature is not raw Ed25519"

test_bootstrap=$tmp/get.sh
cp -- "$bootstrap" "$test_bootstrap"
sed -i \
    -e "s|^bootstrap_release_public_key_sha256=.*|bootstrap_release_public_key_sha256=$public_sha|" \
    -e "s|^release_public_key=.*|release_public_key=$tmp/installed-trust/release-signing-ed25519.pem|" \
    -e "s|^release_sequence_floor=.*|release_sequence_floor=$tmp/release-state/sequence.floor|" \
    -e "s|^signed_update_lock=.*|signed_update_lock=$tmp/release-state/update.lock|" \
    -e "s|^releases_root=.*|releases_root=$tmp/releases|" \
    -e 's|for directory in /var/backups /var/backups/celikpanel "$releases_root"; do|for directory in "$releases_root"; do|' \
    "$test_bootstrap"
grep -Fxq "bootstrap_release_public_key_sha256=$public_sha" "$test_bootstrap" \
    || fail "test bootstrap public-key pin was not replaced exactly"
grep -Fxq "releases_root=$tmp/releases" "$test_bootstrap" \
    || fail "test bootstrap release root was not isolated"
grep -Fq 'for directory in "$releases_root"; do' "$test_bootstrap" \
    || fail "test bootstrap release-storage loop was not isolated"

fake_bin=$tmp/fake-bin
mkdir "$fake_bin"
cat > "$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ -n ${S5_ADMISSION_CURL_TRACE:-} ]]; then
    : > "$S5_ADMISSION_CURL_TRACE"
fi
output=
url=
write_status=0
while (($#)); do
    case "$1" in
        -o)
            output=$2
            shift 2
            ;;
        --write-out)
            write_status=1
            shift 2
            ;;
        --proto|--connect-timeout|--retry|--max-filesize)
            shift 2
            ;;
        --fail|--show-error|--silent|--location|--tlsv1.2)
            shift
            ;;
        https://celikpanel.net/*)
            url=$1
            shift
            ;;
        *)
            printf 'unexpected curl argument: %s\n' "$1" >&2
            exit 2
            ;;
    esac
done
[[ -n $output && -n $url ]]
relative=${url#https://celikpanel.net/}
source_path=$S3_ENTRY_SITE_ROOT/$relative
[[ -f $source_path && ! -L $source_path ]]
cp -- "$source_path" "$output"
((write_status == 0)) || printf '200'
EOF
chmod 0755 "$fake_bin/curl"

failures=0
reset_signed_admission_targets() {
    local target
    for target in "$tmp/releases" "$tmp/release-state" \
        "$tmp/signed-admission-curl.trace"; do
        case "$target" in
            "$tmp"/*) ;;
            *) fail "refusing to reset an admission target outside the fixture" ;;
        esac
        rm -rf -- "$target"
    done
}

expect_signed_admission_refusal() {
    local label=$1 expected=$2 expected_tr=$3 log=$4
    shift 4
    local status target
    reset_signed_admission_targets
    set +e
    "$@" </dev/null >"$log" 2>&1
    status=$?
    set -e
    if [[ $status -ne 0 && $status -ne 86 ]] &&
       grep -Fq -- "$expected" "$log" &&
       grep -Fq -- "$expected_tr" "$log" &&
       grep -Fq 'CELIKPANEL_ADMIN_CREDENTIALS_FILE' "$log" &&
       grep -Fq -- '--admin-credentials-file=-' "$log"; then
        for target in "$tmp/releases" "$tmp/release-state" \
            "$tmp/signed-admission-curl.trace"; do
            if [[ -e $target || -L $target ]]; then
                printf 'ADMISSION_REFUSAL_FAIL label=%s mutated=%s\n' \
                    "$label" "$target" >&2
                failures=$((failures + 1))
                reset_signed_admission_targets
                return
            fi
        done
        printf 'ADMISSION_REFUSAL_PASS label=%s exit=%s\n' "$label" "$status"
    else
        printf 'ADMISSION_REFUSAL_FAIL label=%s exit=%s expected=%s\n' \
            "$label" "$status" "$expected" >&2
        sed 's/^/  | /' "$log" >&2
        for target in "$tmp/releases" "$tmp/release-state" \
            "$tmp/signed-admission-curl.trace"; do
            if [[ -e $target || -L $target ]]; then
                printf '  | MUTATED=%s\n' "$target" >&2
            fi
        done
        failures=$((failures + 1))
    fi
    reset_signed_admission_targets
}

expect_entry_reached() {
    local label=$1 expected_mode=$2 log=$3
    shift 3
    local status
    set +e
    "$@" >"$log" 2>&1
    status=$?
    set -e
    if [[ $status -eq 86 ]] &&
       grep -Fq "S3_ENTRY_BOUNDARY_REACHED mode=$expected_mode root=" "$log"; then
        printf 'ENTRY_PASS label=%s exit=%s\n' "$label" "$status"
    else
        printf 'ENTRY_FAIL label=%s exit=%s\n' "$label" "$status" >&2
        sed 's/^/  | /' "$log" >&2
        failures=$((failures + 1))
    fi
}

expect_guard_refusal() {
    local label=$1 expected=$2 log=$3
    shift 3
    local status
    set +e
    "$@" >"$log" 2>&1
    status=$?
    set -e
    if [[ $status -ne 0 && $status -ne 86 ]] &&
       grep -Fq "$expected" "$log" &&
       ! grep -Fq 'S3_ENTRY_BOUNDARY_REACHED' "$log"; then
        printf 'REFUSAL_PASS label=%s exit=%s message=%s\n' \
            "$label" "$status" "$expected"
    else
        printf 'REFUSAL_FAIL label=%s exit=%s expected=%s\n' \
            "$label" "$status" "$expected" >&2
        sed 's/^/  | /' "$log" >&2
        failures=$((failures + 1))
    fi
}

clean_path=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
signed_narrow_credentials=$tmp/signed-narrow-admin.credentials
signed_invalid_secure_credentials=$tmp/signed-invalid-secure-admin.credentials
signed_reordered_credentials=$tmp/signed-reordered-admin.credentials
signed_public_credentials=$tmp/signed-public-admin.credentials
printf '%s\n' \
    '{"username":"first-admin","email":"admin@example.test","password":"never-print-this-password"}' \
    > "$signed_narrow_credentials"
printf '%s\n' '{"username":"first-admin"' > "$signed_invalid_secure_credentials"
printf '%s\n' \
    '{"email":"admin@example.test","username":"first-admin","password":"never-print-this-password"}' \
    > "$signed_reordered_credentials"
cp -- "$signed_narrow_credentials" "$signed_public_credentials"
chmod 0600 "$signed_narrow_credentials" "$signed_invalid_secure_credentials" \
    "$signed_reordered_credentials"
chmod 0644 "$signed_public_credentials"

partial_zero_admin_db=$tmp/partial-zero-admin.db
partial_usable_admin_db=$tmp/partial-usable-admin.db
/usr/bin/python3 - "$partial_zero_admin_db" "$partial_usable_admin_db" <<'PY'
import sqlite3
import sys

schema = """
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    email TEXT NOT NULL,
    role TEXT NOT NULL,
    account_type TEXT,
    status TEXT
)
"""
for database_path in sys.argv[1:]:
    connection = sqlite3.connect(database_path)
    connection.execute(schema)
    connection.commit()
    connection.close()

connection = sqlite3.connect(sys.argv[2])
connection.execute(
    """
    INSERT INTO users
        (username, password_hash, email, role, account_type, status)
    VALUES
        ('existing-admin',
         '$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$a2V5',
         'existing-admin@example.test', 'admin', 'account', 'active')
    """
)
connection.commit()
connection.close()
PY
chmod 0600 "$partial_zero_admin_db" "$partial_usable_admin_db"
chown root:root "$partial_zero_admin_db" "$partial_usable_admin_db"

make_partial_state_bootstrap() {
    local database_path=$1 output_path=$2
    cp -- "$test_bootstrap" "$output_path"
    sed -i "s|/var/lib/celikpanel/celikpanel.db|$database_path|g" "$output_path"
    [[ $(grep -Fc "$database_path" "$output_path") -eq 2 &&
       $(grep -Fc '/var/lib/celikpanel/celikpanel.db' "$output_path") -eq 0 ]] \
        || fail "partial-state bootstrap did not isolate the database path exactly"
}
partial_zero_admin_bootstrap=$tmp/get-partial-zero-admin.sh
partial_usable_admin_bootstrap=$tmp/get-partial-usable-admin.sh
make_partial_state_bootstrap "$partial_zero_admin_db" "$partial_zero_admin_bootstrap"
make_partial_state_bootstrap "$partial_usable_admin_db" "$partial_usable_admin_bootstrap"

expect_signed_admission_refusal signed-default-non-tty \
    'requires a terminal' 'Etkileşimsiz' "$tmp/signed-default-non-tty.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$test_bootstrap" --install
expect_signed_admission_refusal signed-fresh-skip-admin \
    'A fresh signed public installation cannot use SKIP_ADMIN=1 without credentials' \
    'Yeni imzalı genel kurulum' "$tmp/signed-fresh-skip-admin.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root SKIP_ADMIN=1 \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$test_bootstrap" --install

expect_signed_admission_refusal signed-relative-credentials \
    'must be an absolute canonical root:root mode 0600 regular file' \
    'Yönetici kimlik bilgisi dosyası' "$tmp/signed-relative-credentials.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root \
    CELIKPANEL_ADMIN_CREDENTIALS_FILE=relative-admin.credentials \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$test_bootstrap" --install
expect_signed_admission_refusal signed-public-credentials \
    'must be an absolute canonical root:root mode 0600 regular file' \
    'Yönetici kimlik bilgisi dosyası' "$tmp/signed-public-credentials.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root \
    CELIKPANEL_ADMIN_CREDENTIALS_FILE="$signed_public_credentials" \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$test_bootstrap" --install

expect_signed_admission_refusal signed-invalid-secure-credentials \
    "does not match the signed entry's restricted first-write-safe JSON form" \
    'dar ilk-yazım-güvenli JSON biçimine uymuyor' \
    "$tmp/signed-invalid-secure-credentials.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root \
    CELIKPANEL_ADMIN_CREDENTIALS_FILE="$signed_invalid_secure_credentials" \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$test_bootstrap" --install
expect_signed_admission_refusal signed-reordered-valid-credentials-narrowing \
    'other candidate-valid JSON forms are intentionally not accepted here' \
    'diğer JSON biçimleri burada bilinçli olarak kabul edilmez' \
    "$tmp/signed-reordered-valid-credentials-narrowing.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root \
    CELIKPANEL_ADMIN_CREDENTIALS_FILE="$signed_reordered_credentials" \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$test_bootstrap" --install

expect_entry_reached direct direct "$tmp/direct.log" \
    /usr/bin/env -i PATH="$clean_path" HOME=/root \
    CELIKPANEL_ENTRY_BOUNDARY_EXPECT_MODE=direct \
    CELIKPANEL_ENTRY_BOUNDARY_EXPECT_ROOT="$release_root" \
    /bin/bash "$release_root/install.sh"

expect_signed_admission_refusal signed-explicit-repair-zero-admin \
    'cannot prove a login-capable existing administrator' \
    'oturum açabilen mevcut yöneticiyi kanıtlayamadığı' \
    "$tmp/signed-explicit-repair-zero-admin.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root SKIP_ADMIN=1 \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$partial_zero_admin_bootstrap" --install
expect_signed_admission_refusal signed-explicit-repair-usable-admin-narrowing \
    'cannot prove a login-capable existing administrator' \
    'oturum açabilen mevcut yöneticiyi kanıtlayamadığı' \
    "$tmp/signed-explicit-repair-usable-admin-narrowing.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root SKIP_ADMIN=1 \
    S3_ENTRY_SITE_ROOT="$site" \
    S5_ADMISSION_CURL_TRACE="$tmp/signed-admission-curl.trace" \
    /bin/sh "$partial_usable_admin_bootstrap" --install

expect_entry_reached signed-public-narrow-credentials signed \
    "$tmp/signed-public-narrow-credentials.log" \
    /usr/bin/env -i PATH="$fake_bin:$clean_path" HOME=/root \
    CELIKPANEL_ADMIN_CREDENTIALS_FILE="$signed_narrow_credentials" \
    S3_ENTRY_SITE_ROOT="$site" \
    CELIKPANEL_ENTRY_BOUNDARY_EXPECT_MODE=signed \
    CELIKPANEL_ENTRY_BOUNDARY_EXPECT_PREFIX="$tmp/releases" \
    /bin/sh "$test_bootstrap" --install </dev/null

expect_entry_reached signed-public-tty signed "$tmp/signed-public-tty.log" \
    /usr/bin/script -qefc \
    "/usr/bin/env -i PATH=$fake_bin:$clean_path HOME=/root S3_ENTRY_SITE_ROOT=$site CELIKPANEL_ENTRY_BOUNDARY_EXPECT_MODE=signed CELIKPANEL_ENTRY_BOUNDARY_EXPECT_PREFIX=$tmp/releases /bin/sh $test_bootstrap --install" \
    /dev/null

foreign_root=$tmp/foreign-root
mkdir "$foreign_root"
expect_guard_refusal foreign-canonical-root \
    'release transaction guard is outside the verified running release' \
    "$tmp/foreign.log" \
    /usr/bin/env -i PATH="$clean_path" HOME=/root \
    CELIKPANEL_TRUSTED_RELEASE_ROOT="$foreign_root" \
    /bin/bash "$release_root/install.sh"

expect_guard_refusal relative-root \
    'trusted release root is missing while sourcing release guard' \
    "$tmp/relative.log" \
    /usr/bin/env -i PATH="$clean_path" HOME=/root \
    CELIKPANEL_TRUSTED_RELEASE_ROOT=relative-root \
    /bin/bash "$release_root/install.sh"

ln -s "$release_root" "$tmp/release-alias"
expect_guard_refusal symlink-root \
    'release transaction guard is outside the verified running release' \
    "$tmp/symlink.log" \
    /usr/bin/env -i PATH="$clean_path" HOME=/root \
    CELIKPANEL_TRUSTED_RELEASE_ROOT="$tmp/release-alias" \
    /bin/bash "$release_root/install.sh"

expect_guard_refusal trailing-slash-root \
    'release transaction guard is outside the verified running release' \
    "$tmp/trailing-slash.log" \
    /usr/bin/env -i PATH="$clean_path" HOME=/root \
    CELIKPANEL_TRUSTED_RELEASE_ROOT="$release_root/" \
    /bin/bash "$release_root/install.sh"

expect_guard_refusal apply-only-missing-root \
    'trusted release root is missing while sourcing release guard' \
    "$tmp/apply-only.log" \
    /usr/bin/env -i PATH="$clean_path" HOME=/root \
    CELIKPANEL_APPLY_ONLY=1 \
    /bin/bash "$release_root/install.sh"

((failures == 0)) || fail "$failures entry-boundary assertion(s) failed"
printf 'fresh-install entry-boundary contract passed\n'
