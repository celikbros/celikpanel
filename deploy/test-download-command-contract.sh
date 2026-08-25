#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
index=$repo_root/download-portal/index.html
site_js=$repo_root/download-portal/assets/site.js

fail() {
  printf 'download command contract failed: %s\n' "$1" >&2
  exit 1
}

for command in awk cmp dash grep node; do
  command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM

awk '
  /\/\/ BEGIN DOWNLOAD COMMAND POLICY/ { capture=1; next }
  /\/\/ END DOWNLOAD COMMAND POLICY/ { capture=0; next }
  capture { print }
' "$site_js" > "$tmp/policy.js"
[[ -s "$tmp/policy.js" ]] || fail 'JavaScript command policy was not extracted'
cat >> "$tmp/policy.js" <<'EOF'
const requestedVersion = process.argv[2] || "";
process.stdout.write(buildInstallCommand(requestedVersion) + "\n");
EOF

node "$tmp/policy.js" > "$tmp/latest.expected"
node "$tmp/policy.js" v0.1.0-alpha.41 > "$tmp/exact.expected"
dash -n "$tmp/latest.expected" || fail 'latest command is not POSIX shell syntax'
dash -n "$tmp/exact.expected" || fail 'exact command is not POSIX shell syntax'
if node "$tmp/policy.js" 'v0.1.0-alpha.41;touch-pwned' >/dev/null 2>&1; then
  fail 'shell-active release version was accepted'
fi
if node "$tmp/policy.js" v0.1.0-alpha.041 >/dev/null 2>&1; then
  fail 'non-canonical numeric prerelease was accepted'
fi

awk '
  /<code id="latest-command">/ {
    capture=1
    sub(/^.*<code id="latest-command">/, "")
  }
  capture {
    if (/<\/code>/) {
      sub(/<\/code>.*$/, "")
      print
      exit
    }
    print
  }
' "$index" > "$tmp/latest.html"
cmp -s "$tmp/latest.expected" "$tmp/latest.html" \
  || fail 'static and JavaScript latest commands differ'

for rendered in "$tmp/latest.expected" "$tmp/exact.expected"; do
  grep -Fq 'celikpanel_get=$(mktemp)' "$rendered" \
    || fail 'command does not create an unpredictable temporary file'
  grep -Fq 'trap cleanup_celikpanel_get EXIT' "$rendered" \
    || fail 'command does not clean the temporary file on exit'
  grep -Fq -- "--proto '=https'" "$rendered" \
    || fail 'command does not require HTTPS'
  grep -Fq 'https://celikpanel.net/get.sh' "$rendered" \
    || fail 'command does not pin the CelikPanel HTTPS origin'
  grep -Fq 'if [ "$(id -u)" -eq 0 ]; then' "$rendered" \
    || fail 'command does not distinguish root from an unprivileged user'
  grep -Fq 'sudo sh "$celikpanel_get"' "$rendered" \
    || fail 'command has no sudo-enabled user path'
  if grep -Eq '/tmp/|/var/tmp/|celikpanel-get\.sh' "$rendered"; then
    fail 'command contains a fixed temporary pathname'
  fi
done
grep -Fq 'sh "$celikpanel_get" --version "v0.1.0-alpha.41"' "$tmp/exact.expected" \
  || fail 'exact command does not pass a quoted canonical release version'

fake_bin=$tmp/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/mktemp" <<'EOF'
#!/bin/sh
set -eu
stage=$TEST_ROOT/stage-$TEST_CASE
(umask 077; set -C; : > "$stage")
printf '%s\n' "$stage" > "$TEST_ROOT/stage-path-$TEST_CASE"
printf '%s\n' "$stage"
EOF
cat > "$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$@" > "$TEST_ROOT/curl-args-$TEST_CASE"
destination=
previous=
for argument in "$@"; do
  if [ "$previous" = -o ]; then destination=$argument; fi
  previous=$argument
done
[ -n "$destination" ] && [ -f "$destination" ] && [ ! -L "$destination" ]
[ "$(cat "$TEST_ROOT/stage-path-$TEST_CASE")" = "$destination" ]
if [ "${TEST_CURL_FAIL:-0}" -eq 1 ]; then exit 22; fi
printf '%s\n' verified-bootstrap > "$destination"
EOF
cat > "$fake_bin/id" <<'EOF'
#!/bin/sh
set -eu
[ "$#" -eq 1 ] && [ "$1" = -u ]
printf '%s\n' "$TEST_UID"
EOF
cat > "$fake_bin/sh" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$@" > "$TEST_ROOT/sh-args-$TEST_CASE"
[ -f "$1" ] && [ ! -L "$1" ]
[ "$(cat "$1")" = verified-bootstrap ]
EOF
cat > "$fake_bin/sudo" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$@" > "$TEST_ROOT/sudo-args-$TEST_CASE"
[ "$1" = sh ]
shift
exec "$TEST_FAKE_BIN/sh" "$@"
EOF
chmod 0755 "$fake_bin"/*

run_rendered() {
  local rendered=$1 test_case=$2 uid=$3
  TEST_ROOT=$tmp TEST_CASE=$test_case TEST_UID=$uid TEST_FAKE_BIN=$fake_bin \
    PATH=$fake_bin:/usr/bin:/bin /bin/sh "$rendered"
  local stage
  stage=$(cat "$tmp/stage-path-$test_case")
  [[ ! -e "$stage" && ! -L "$stage" ]] \
    || fail "$test_case retained its temporary bootstrap"
}

run_rendered "$tmp/latest.expected" root 0
[[ ! -e "$tmp/sudo-args-root" ]] || fail 'root command invoked sudo'
[[ "$(wc -l < "$tmp/sh-args-root")" -eq 1 ]] \
  || fail 'root latest command passed unexpected arguments'

run_rendered "$tmp/exact.expected" sudo 1000
grep -Fxq sh "$tmp/sudo-args-sudo" || fail 'unprivileged command did not invoke sudo sh'
grep -Fxq -- --version "$tmp/sh-args-sudo" \
  || fail 'sudo exact command omitted --version'
grep -Fxq v0.1.0-alpha.41 "$tmp/sh-args-sudo" \
  || fail 'sudo exact command changed the release version'

TEST_ROOT=$tmp TEST_CASE=curl-failure TEST_UID=1000 TEST_FAKE_BIN=$fake_bin \
  TEST_CURL_FAIL=1 PATH=$fake_bin:/usr/bin:/bin \
  /bin/sh "$tmp/latest.expected" >/dev/null 2>&1 && \
  fail 'download failure did not fail closed'
failed_stage=$(cat "$tmp/stage-path-curl-failure")
[[ ! -e "$failed_stage" && ! -L "$failed_stage" ]] \
  || fail 'download failure retained its temporary bootstrap'
[[ ! -e "$tmp/sh-args-curl-failure" && ! -e "$tmp/sudo-args-curl-failure" ]] \
  || fail 'download failure reached privileged execution'

for required_curl_argument in --fail --show-error --location '=https' --tlsv1.2 \
  https://celikpanel.net/get.sh -o; do
  grep -Fxq -- "$required_curl_argument" "$tmp/curl-args-root" \
    || fail "curl argument is missing: $required_curl_argument"
done

printf 'download command contract passed\n'
