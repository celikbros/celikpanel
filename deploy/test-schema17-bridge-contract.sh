#!/bin/bash
# Static contract for the one-time exact schema-17 to schema-20 release bridge.
# The bridge is intentionally closed over one source shape, one migration span
# and one manifest-verified rollback path.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
BOOTSTRAP="$ROOT/bootstrap-update.sh"
UPDATE="$ROOT/update.sh"
ROLLBACK="$ROOT/rollback.sh"
GUARD="$ROOT/deploy/release-transaction-guard.sh"
HELPER="$ROOT/deploy/schema17bridge/main.go"

die() {
    echo "schema17 bridge contract failed: $*" >&2
    exit 1
}

require_literal() {
    local file=$1 literal=$2
    grep -Fq -- "$literal" "$file" || die "$(basename "$file") is missing: $literal"
}

reject_literal() {
    local file=$1 literal=$2
    ! grep -Fq -- "$literal" "$file" || die "$(basename "$file") must not contain: $literal"
}

require_count() {
    local file=$1 literal=$2 expected=$3 actual
    actual=$(grep -F -c -- "$literal" "$file" || true)
    [[ "$actual" == "$expected" ]] \
        || die "$(basename "$file") count for '$literal' is $actual, want $expected"
}

require_sequence() {
    local file=$1 cursor=0 literal line
    shift
    for literal in "$@"; do
        line=$({ grep -Fn -- "$literal" "$file" || true; } \
            | awk -F: -v cursor="$cursor" '$1 > cursor { print $1; exit }')
        [[ "$line" =~ ^[0-9]+$ ]] \
            || die "$(basename "$file") has no ordered marker after line $cursor: $literal"
        cursor=$line
    done
}

bash -n "$BOOTSTRAP" "$UPDATE" "$ROLLBACK" "$GUARD"

# The immutable release contains the dedicated helper and recognizes the mode.
require_literal "$BOOTSTRAP" '--normal|--bootstrap-pre-ledger|--bootstrap-schema17) UPDATE_MODE=$1'
require_literal "$BOOTSTRAP" 'run_clean "$go_bin" build -trimpath -buildvcs=false -ldflags "-s -w" -o bin/schema17-bridge ./deploy/schema17bridge'
require_literal "$BOOTSTRAP" '[[ -x "$root/bin/schema17-bridge" && -f "$root/bin/schema17-bridge" ]]'
require_literal "$BOOTSTRAP" '"$incomplete_root/bin/schema17-bridge"'
require_literal "$UPDATE" 'SCHEMA17_BRIDGE="$root/bin/schema17-bridge"'
require_literal "$UPDATE" 'validate_preflight_binary "$SCHEMA17_BRIDGE" schema17-bridge'
require_literal "$UPDATE" '--bootstrap-schema17)'
require_literal "$UPDATE" '[[ $BOOTSTRAP_SCHEMA17 -ne 1 ]] || snapshot_schema=schema17'
require_literal "$GUARD" '! printf '\''schema17\n'\'' | cmp -s - "$transition"'

# Exact-17 is re-proved before quiesce, while frozen, and after both services stop.
require_count "$UPDATE" '"$SCHEMA17_BRIDGE" check --db "$PANEL_DB"' 3
require_sequence "$UPDATE" \
    'panel is not at the exact supported schema version 17' \
    'freeze_release_service_cgroup celikpanel-panel.service panel panel_frozen' \
    'final frozen panel exact schema17 proof failed' \
    'terminate_frozen_release_service celikpanel-panel.service panel panel_frozen' \
    'stopped panel exact schema17 proof failed'

# The exact snapshot and provenance become durable before the first DB mutation.
require_literal "$UPDATE" 'cp -a "$SCHEMA17_BRIDGE" "$tmp_snap/transition-preflight/schema17-bridge"'
require_literal "$UPDATE" 'mode bootstrap-schema17'
require_literal "$UPDATE" 'source-schema-version 17'
require_literal "$UPDATE" 'bridge-schema-version 20'
require_literal "$UPDATE" 'sha256sum schema17-transition.tsv > schema17-transition.sha256'
require_sequence "$UPDATE" \
    '"$SCHEMA17_BRIDGE" snapshot \' \
    'sha256sum schema17-transition.tsv > schema17-transition.sha256' \
    'LC_ALL=C find . -type f ! -path '\''./SHA256SUMS'\'' -print0' \
    'mv -T --no-clobber -- "$tmp_snap" "$snap"' \
    'verified_snapshot=$snap' \
    'active marker changed before exact schema17 bridge migration' \
    '"$SCHEMA17_BRIDGE" migrate \' \
    '--migrations-root "$TRUSTED_RELEASE_ROOT/internal/db/migrations"' \
    'schema17 bridge did not produce the exact idle schema20 state' \
    'cannot release bootstrap mutation lock before ledger initialization' \
    '"$TRUSTED_RELEASE_ROOT/bin/agent" --initialize-service-mutation-ledger'

# Rollback accepts only exact, manifest-covered schema17 transition metadata and
# restores through the snapshot-carried helper after the rollback marker is active.
require_literal "$ROLLBACK" 'PREFLIGHT_SCHEMA17_BRIDGE="$snap/transition-preflight/schema17-bridge"'
require_literal "$ROLLBACK" '[[ "${#schema17_values[@]}" -eq 8 ]]'
require_literal "$ROLLBACK" '[[ "${schema17_values[source-schema-version]}" == 17 ]]'
require_literal "$ROLLBACK" '[[ "${schema17_values[bridge-schema-version]}" == 20 ]]'
require_literal "$ROLLBACK" 'validate_preflight_binary "$PREFLIGHT_SCHEMA17_BRIDGE" schema17-bridge'
require_literal "$ROLLBACK" '"$PREFLIGHT_SCHEMA17_BRIDGE" restore \'
require_sequence "$ROLLBACK" \
    'sha256sum -c SHA256SUMS >/dev/null' \
    'transition_state=$(cat "$snap/snapshot-transition.state")' \
    'PREFLIGHT_SCHEMA17_BRIDGE="$snap/transition-preflight/schema17-bridge"' \
    '"$PREFLIGHT_SCHEMA17_BRIDGE" check \' \
    'rollback_verified_snapshot=$snap' \
    'systemctl stop celikpanel-panel.service' \
    'rollback_mutation_started=1' \
    'active rollback marker changed before exact schema17 restore' \
    '"$PREFLIGHT_SCHEMA17_BRIDGE" restore \' \
    'restored schema17 panel database is not exact schema version 17'
reject_literal "$ROLLBACK" 'cp -a "$snap/$(basename "$PANEL_DB")"'

# The helper itself is closed over exact schema 17, migrations 18..20 and four
# explicit commands; its Go tests exercise rejection and restore behavior.
require_literal "$HELPER" 'sourceSchemaVersion = 17'
require_literal "$HELPER" 'bridgeSchemaVersion = 20'
require_literal "$HELPER" '18: "018_hostname_namespace.sql"'
require_literal "$HELPER" '19: "019_hsts_retirement.sql"'
require_literal "$HELPER" '20: "020_ssl_lineage_identity.sql"'
require_literal "$HELPER" 'case "check":'
require_literal "$HELPER" 'case "snapshot":'
require_literal "$HELPER" 'case "migrate":'
require_literal "$HELPER" 'case "restore":'
require_literal "$HELPER" 'PRAGMA foreign_key_check'
require_literal "$HELPER" 'VACUUM INTO ?'

echo "schema17 bridge contract: ok"
