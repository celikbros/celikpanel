#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
builder="$repo_root/deploy/build-download-portal.sh"
bootstrap="$repo_root/download-portal/get.sh"

fail() {
  printf 'download portal contract failed: %s\n' "$1" >&2
  exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM

version=v1.2.3-alpha.4
commit=0123456789abcdef0123456789abcdef01234567
published_at=2026-08-03T08:55:11Z
archive="celikpanel-$version.tar.gz"
mkdir -p "$tmp/source/celikpanel-$version"
printf '#!/bin/sh\nprintf test-install\\n\n' > "$tmp/source/celikpanel-$version/install.sh"
tar -czf "$tmp/$archive" -C "$tmp/source" "celikpanel-$version"
(cd "$tmp" && sha256sum "$archive" > "$archive.sha256")

bash "$builder" "$version" "$commit" "$published_at" \
  "$tmp/$archive" "$tmp/$archive.sha256" "$tmp/site"

[[ -f "$tmp/site/index.html" ]] || fail "home page was not generated"
[[ -x "$tmp/site/get.sh" ]] || fail "bootstrap is not executable"
[[ -f "$tmp/site/releases/$version/$archive" ]] || fail "versioned archive is missing"
[[ "$(cat "$tmp/site/releases/latest.txt")" == "$version" ]] || fail "latest pointer is wrong"
cmp "$tmp/$archive" "$tmp/site/releases/$version/$archive" || fail "archive bytes changed"

python3 - "$tmp/site/releases/latest.json" "$tmp/site/releases/index.json" <<'PY'
import json
import pathlib
import sys

latest = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
index = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
assert latest["version"] == "v1.2.3-alpha.4"
assert latest["commit"] == "0123456789abcdef0123456789abcdef01234567"
assert len(latest["sha256"]) == 64
assert index["latest"] == latest["version"]
assert index["releases"] == [latest]
PY

grep -Fq "Options -Indexes" "$tmp/site/.htaccess" || fail "directory listing is not disabled"
grep -Fq "Content-Security-Policy" "$tmp/site/.htaccess" || fail "CSP header is missing"
grep -Fq -- "--proto '=https'" "$bootstrap" || fail "HTTPS protocol restriction is missing"
grep -Fq "sha256sum -c" "$bootstrap" || fail "archive checksum is not verified"
grep -Fq "exact internal checksum manifest" "$bootstrap" || fail "exact internal manifest validation is missing"
grep -Fq "release.commit release.tree" "$bootstrap" || fail "release provenance validation is missing"
grep -Fq "Archive contains unsafe or unexpected paths" "$bootstrap" || fail "path validation is missing"
grep -Fq "Arşiv güvensiz veya beklenmeyen yollar içeriyor" "$bootstrap" || fail "Turkish path validation is missing"
grep -Fq 'for required_command in awk bash chmod chown cmp curl dirname env find grep id install' "$bootstrap" \
  || fail "complete runtime requirement gate is missing"
grep -Fq 'bash "$installer"' "$bootstrap" || fail "installer is not executed with bash"
grep -Fq -- '--install|--update' "$bootstrap" || fail "install/update selector is missing"
grep -Fq '/etc/celikpanel/install.complete' "$bootstrap" || fail "completion marker gate is missing"
grep -Fq 'A partial or ambiguous CelikPanel installation was found.' "$bootstrap" \
  || fail "partial installation refusal is missing"
grep -Fq '# BEGIN DOWNLOAD OPERATION POLICY' "$bootstrap" \
  || fail "extractable download operation policy is missing"
grep -Fq 'detect_known_interrupted_update_candidate_at()' "$bootstrap" \
  || fail "interrupted-update detector is missing"
grep -Fq '/var/lib/celikpanel-release-transaction' "$bootstrap" \
  || fail "fixed production transaction root is missing"
grep -Fq '8bbbac8b628fae4fca0e127e52c1c7835f56f8b8' "$bootstrap" \
  || fail "known interrupted alpha.4 target is missing"
grep -Fq 'select_download_operation()' "$bootstrap" \
  || fail "pure download operation selector is missing"
grep -Fq 'recovery-update' "$bootstrap" \
  || fail "narrow recovery-update classification is missing"
[[ "$(grep -Fc 'detect_known_interrupted_update_candidate || fail' "$bootstrap")" -eq 2 ]] \
  || fail "interrupted-update evidence is not rechecked exactly twice"
grep -Fq 'bootstrap-prebuilt-update.sh' "$bootstrap" || fail "prebuilt updater is missing"
grep -Fq 'Updating CelikPanel to' "$bootstrap" || fail "English update progress is missing"
grep -Fq "Usage:" "$bootstrap" || fail "English usage text is missing"
grep -Fq "Kullanım:" "$bootstrap" || fail "Turkish usage text is missing"
grep -Fq "Installing CelikPanel" "$bootstrap" || fail "English install progress is missing"
grep -Fq "doğrulanmış" "$bootstrap" || fail "Turkish install progress is missing"
if grep -Eq 'curl[^\n]*\|[[:space:]]*(ba)?sh' "$repo_root/download-portal/index.html"; then
  fail "home page recommends curl-pipe-shell"
fi

if bash "$builder" "$version" "$commit" "$published_at" \
  "$tmp/$archive" "$tmp/$archive.sha256" "$tmp/site" >/dev/null 2>&1; then
  fail "builder overwrote an existing output directory"
fi

printf 'download portal contract passed\n'
