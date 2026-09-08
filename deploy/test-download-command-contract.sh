#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
index=$repo_root/download-portal/index.html
site_js=$repo_root/download-portal/assets/site.js
fail() { printf 'download command contract failed: %s\n' "$1" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
awk '/\/\/ BEGIN DOWNLOAD COMMAND POLICY/ { capture=1; next } /\/\/ END DOWNLOAD COMMAND POLICY/ { capture=0; next } capture { print }' "$site_js" > "$tmp/policy.js"
printf '\nprocess.stdout.write(buildInstallCommand() + "\\n");\n' >> "$tmp/policy.js"
node "$tmp/policy.js" > "$tmp/command"
dash -n "$tmp/command"
[[ $(wc -l < "$tmp/command") == 2 ]] || fail 'public command is not two lines'
python3 - "$index" > "$tmp/html-command" <<'PY'
import sys,html,re
text=open(sys.argv[1],encoding='utf-8').read()
matches=re.findall(r'<code id="latest-command">(.*?)</code>',text,re.S)
assert len(matches)==1
assert 'exact-command' not in text and 'command-disclosure' not in text
assert text.count('data-copy="latest-command"')==1
assert 'root terminalinde' in text and 'root terminal.' in text
print(html.unescape(matches[0]))
PY
cmp "$tmp/command" "$tmp/html-command" || fail 'copied and visible commands differ'
mkdir "$tmp/bin"
cat > "$tmp/bin/curl" <<'SH'
#!/bin/sh
printf '%s\n' "$@" > "$TEST_ROOT/curl-args"
if [ "${TEST_FAIL:-0}" = 1 ]; then
  printf '%s\n' partial-download > celikpanel-install.sh
  exit 22
fi
printf '%s\n' downloaded-bootstrap > celikpanel-install.sh
SH
cat > "$tmp/bin/sh" <<'SH'
#!/bin/sh
[ "$#" = 1 ] && [ "$1" = celikpanel-install.sh ] || exit 2
[ "$(cat "$1")" = downloaded-bootstrap ] || exit 3
touch "$TEST_ROOT/executed"
SH
chmod +x "$tmp/bin/curl" "$tmp/bin/sh"
(cd "$tmp"; TEST_ROOT=$tmp PATH=$tmp/bin:/usr/bin:/bin dash "$tmp/command")
[[ -f "$tmp/executed" ]] || fail 'successful download did not run'
for argument in -fL --proto '=https' -o celikpanel-install.sh https://celikpanel.net/get.sh; do
  grep -Fxq -- "$argument" "$tmp/curl-args" || fail "missing curl argument: $argument"
done
rm "$tmp/executed"
if (cd "$tmp"; TEST_ROOT=$tmp TEST_FAIL=1 PATH=$tmp/bin:/usr/bin:/bin dash "$tmp/command"); then
  fail 'download failure was not propagated'
fi
[[ ! -e "$tmp/executed" ]] || fail 'failed download executed a stale or partial file'
printf 'download command contract passed\n'
