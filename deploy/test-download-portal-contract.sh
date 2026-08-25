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
[[ -f "$tmp/site/assets/site.js" ]] || fail "home page script was not generated"
[[ -f "$tmp/site/.well-known/security.txt" ]] || fail "security.txt was not generated"
[[ -x "$tmp/site/get.sh" ]] || fail "bootstrap is not executable"
[[ -f "$tmp/site/release-signing-ed25519.pem" ]] || fail "release public key was not generated"
[[ "$(sha256sum "$tmp/site/release-signing-ed25519.pem" | awk '{print $1}')" == \
   7eadeb0b156f1a821575c4293fe664b44b8004bcdb5e9e770122cb5c144c68bb ]] \
  || fail "release public key digest is wrong"
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
[[ "$(grep -Fc 'data-language="tr"' "$tmp/site/index.html")" -eq 1 ]] \
  || fail "Turkish language selector is missing or duplicated"
[[ "$(grep -Fc 'data-language="en"' "$tmp/site/index.html")" -eq 1 ]] \
  || fail "English language selector is missing or duplicated"
python3 - "$tmp/site/index.html" <<'PY'
from html.parser import HTMLParser
import pathlib
import sys

class LanguageButtons(HTMLParser):
    def __init__(self):
        super().__init__()
        self.buttons = {}

    def handle_starttag(self, tag, attrs):
        if tag != "button":
            return
        values = dict(attrs)
        language = values.get("data-language")
        if language:
            self.buttons[language] = values

parser = LanguageButtons()
parser.feed(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
assert parser.buttons["tr"]["lang"] == "tr"
assert parser.buttons["tr"]["aria-label"] == "Türkçe"
assert parser.buttons["en"]["lang"] == "en"
assert parser.buttons["en"]["aria-label"] == "English"
PY
grep -Fq 'celikpanel-language' "$tmp/site/assets/site.js" \
  || fail "language preference persistence is missing"
grep -Fq 'Simple and Modern Hosting Control Panel' "$tmp/site/assets/site.js" \
  || fail "English product copy is missing"
grep -Fq 'document.documentElement.lang = currentLanguage' "$tmp/site/assets/site.js" \
  || fail "document language is not updated"
grep -Fq 'celikbros/celikpanel-feedback/issues/new?template=bug_tr.yml' "$tmp/site/index.html" \
  || fail "Turkish bug report link is missing"
grep -Fq 'celikbros/celikpanel-feedback/issues/new?template=bug_en.yml' "$tmp/site/index.html" \
  || fail "English bug report link is missing"
grep -Fq 'celikbros/celikpanel-feedback/issues/new?template=feature_tr.yml' "$tmp/site/index.html" \
  || fail "Turkish feature request link is missing"
grep -Fq 'celikbros/celikpanel-feedback/issues/new?template=feature_en.yml' "$tmp/site/index.html" \
  || fail "English feature request link is missing"
grep -Fq 'celikbros/celikpanel-feedback/security/advisories/new' "$tmp/site/index.html" \
  || fail "private security report link is missing"
grep -Fq 'data-localized-href' "$tmp/site/assets/site.js" \
  || fail "localized feedback link behavior is missing"
grep -Fq 'Canonical: https://celikpanel.net/.well-known/security.txt' "$tmp/site/.well-known/security.txt" \
  || fail "security.txt canonical URL is missing"
grep -Fq 'Preferred-Languages: tr, en' "$tmp/site/.well-known/security.txt" \
  || fail "security.txt language preference is missing"
if grep -Eq '<button[^>]*>(+ Yeni site|×|Devam et)' "$tmp/site/index.html"; then
  fail "illustrative product controls must not enter the keyboard tab order"
fi
grep -Fq -- "--proto '=https'" "$bootstrap" || fail "HTTPS protocol restriction is missing"
grep -Fxq 'bootstrap_release_sequence=44' "$bootstrap" || fail "bootstrap sequence pin is missing"
grep -Fxq 'bootstrap_release_version=v0.1.0-alpha.44' "$bootstrap" || fail "bootstrap version pin is missing"
grep -Fxq 'bootstrap_release_public_key_sha256=7eadeb0b156f1a821575c4293fe664b44b8004bcdb5e9e770122cb5c144c68bb' "$bootstrap" \
  || fail "bootstrap public-key pin is missing"
grep -Fq "sha256sum -c" "$bootstrap" || fail "archive checksum is not verified"
grep -Fq "exact internal checksum manifest" "$bootstrap" || fail "exact internal manifest validation is missing"
grep -Fq "release.commit release.tree" "$bootstrap" || fail "release provenance validation is missing"
grep -Fq "Archive contains unsafe or unexpected paths" "$bootstrap" || fail "path validation is missing"
grep -Fq "Arşiv güvensiz veya beklenmeyen yollar içeriyor" "$bootstrap" || fail "Turkish path validation is missing"
grep -Fq 'for required_command in awk bash chmod chown cmp curl dirname env find flock grep id install' "$bootstrap" \
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
