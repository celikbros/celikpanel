#!/usr/bin/env bash
set -euo pipefail
umask 077
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
stage=$(mktemp -d /var/tmp/celikpanel-exit-trace.XXXXXXXX)
printf '%s\n' "$stage"
mkdir "$stage/source" "$stage/source/exit-fixture"
for name in native_trace.py test_native_exit_threads.py run-exit-local.sh exit-fixture/main.go; do
    test -f "$source_dir/$name" && test ! -L "$source_dir/$name"
    cp -- "$source_dir/$name" "$stage/source/$name"
done
for name in wal_frames.py wal_migration_identity.py; do
    test -f "$source_dir/../$name" && test ! -L "$source_dir/../$name"
    cp -- "$source_dir/../$name" "$stage/source/$name"
done
cd "$stage/source"
sha256sum native_trace.py test_native_exit_threads.py run-exit-local.sh exit-fixture/main.go wal_frames.py wal_migration_identity.py > "$stage/source.sha256"
go_bin=${CELIKPANEL_WALTRACE_GO:-$(command -v go || true)}
[[ -n $go_bin && -x $go_bin ]] || { printf '%s\n' 'Go 1.26.5 is required; nothing is installed automatically.'; exit 2; }
export GOENV=off GOTOOLCHAIN=local GOWORK=off GOFLAGS= GOPROXY=off GOSUMDB=off
"$go_bin" version > "$stage/toolchain.txt"
[[ $(head -n 1 "$stage/toolchain.txt") == 'go version go1.26.5 linux/amd64' ]] || exit 2
sha256sum "$go_bin" >> "$stage/toolchain.txt"
CGO_ENABLED=0 "$go_bin" build -buildvcs=false -trimpath -o "$stage/exit-race" exit-fixture/main.go > "$stage/build.log" 2>&1
sha256sum "$stage/exit-race" > "$stage/binary.sha256"
set +e
CELIKPANEL_EXIT_TRACE_CHILD="$stage/exit-race" CELIKPANEL_EXIT_TRACE_EVIDENCE="$stage/evidence" python3 -m unittest discover -s . -p test_native_exit_threads.py -v > "$stage/tests.log" 2>&1
result=$?
set -e
printf '%s\n' "$result" > "$stage/tests.exit"
sha256sum --check "$stage/source.sha256" > "$stage/source-recheck.log"
cat "$stage/tests.log"
if [[ -f $stage/evidence/summary.json ]]; then cat "$stage/evidence/summary.json"; fi
exit "$result"
