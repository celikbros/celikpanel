#!/bin/bash
# Run every deploy/test-*.sh of the candidate archive once, each in a fresh extraction.
# Usage: run-pass.sh <pass-name> <work-base> [test-name ...]
set -u
pass=$1; base=$2; shift 2
out=/srv/rt/out/$pass
mkdir -p "$out"
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export GOTOOLCHAIN=local GOPROXY=off LANG=C.UTF-8 LC_ALL=C.UTF-8
export HOME=$(getent passwd "$(id -un)" | cut -d: -f6)
unset GOFLAGS CELIKPANEL_CI_REF_TYPE CELIKPANEL_CI_REF_NAME CELIKPANEL_CI_RELEASE_SEQUENCE
if [ "$#" -gt 0 ]; then
    tests=("$@")
else
    mapfile -t tests < <(tar -tf /srv/rt/src.tar | grep -E '^deploy/test-[^/]*\.sh$' | sed 's#^deploy/##; s#\.sh$##' | sort)
fi
{
    echo "pass=$pass user=$(id -un) uid=$(id -u) home=$HOME"
    echo "started=$(date -u +%FT%TZ) tests=${#tests[@]}"
    go version
    printf 'env: GOTOOLCHAIN=%s GOPROXY=%s LANG=%s\n' "$GOTOOLCHAIN" "$GOPROXY" "$LANG"
} > "$out/pass-info.txt"
: > "$out/results.tsv"
for name in "${tests[@]}"; do
    w=$base/$name
    rm -rf -- "$w"
    mkdir -p -- "$w/src"
    tar -C "$w/src" -xf /srv/rt/src.tar
    stamp=$w/.stamp; touch "$stamp"; sleep 1
    echo "$name" > "$out/CURRENT"
    t0=$(date +%s.%N)
    ( cd "$w/src" && exec timeout -k 30 1800 bash "deploy/$name.sh" ) < /dev/null > "$out/$name.log" 2>&1
    rc=$?
    t1=$(date +%s.%N)
    dur=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}')
    printf '%s\t%s\t%s\n' "$name" "$rc" "$dur" >> "$out/results.tsv"
    if [ "$(id -u)" = 0 ]; then
        find /etc /var/lib /opt /usr/local /run/systemd /srv -xdev -maxdepth 4 -newer "$stamp" \
            -not -path '/srv/rt/*' 2>/dev/null | sort | head -200 > "$out/$name.footprint"
    fi
    rm -rf -- "$w"
done
echo "finished=$(date -u +%FT%TZ)" >> "$out/pass-info.txt"
echo DONE > "$out/DONE"
