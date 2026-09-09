#!/usr/bin/env bash
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo"
work=$(mktemp -d /tmp/celikpanel-membership-suite.XXXXXXXX)
server_pid=
cleanup() {
    if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
    case "$work" in /tmp/celikpanel-membership-suite.*) rm -rf -- "$work" ;; esac
}
trap cleanup EXIT
php_args=()
if ! php -r 'exit(extension_loaded("pdo_sqlite")?0:1);'; then php_args+=(-d extension=pdo_sqlite); fi
if ! php -r 'exit(extension_loaded("sodium")?0:1);'; then php_args+=(-d extension=sodium); fi
while IFS= read -r file; do php "${php_args[@]}" -l "$file" >/dev/null; done < <(find portal-membership download-portal/account -name '*.php' -type f)
export CELIKPANEL_LICENSE_INTEROP_FIXTURE="$work/interop.json"
php "${php_args[@]}" portal-membership/tests/service.php
python3 portal-membership/tests/smtp_test.py
go test ./internal/licensing -count=1
php "${php_args[@]}" portal-membership/tests/prepare.php "$work/http"
export CELIKPANEL_MEMBERSHIP_PRIVATE="$work/http"
php "${php_args[@]}" -S 127.0.0.1:8379 -t download-portal portal-membership/tests/router.php >"$work/server.log" 2>&1 &
server_pid=$!
for attempt in {1..30}; do
    kill -0 "$server_pid" || { cat "$work/server.log"; exit 1; }
    if curl -fsS http://127.0.0.1:8379/account/ >/dev/null 2>&1; then break; fi
    sleep 0.1
done
python3 portal-membership/tests/http_flow_test.py "$work/http"
