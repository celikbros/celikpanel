#!/usr/bin/env bash
set -euo pipefail
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository=$(git -C "$source_dir" rev-parse --show-toplevel)
stage=$(mktemp -d /var/tmp/celikpanel-waltrace.XXXXXXXX)
chmod 700 "$stage"
printf '%s\n' "$stage"
mkdir "$stage/source"
git -C "$repository" archive HEAD > "$stage/source.tar"
git get-tar-commit-id < "$stage/source.tar" > "$stage/base-commit.txt"
tar -xf "$stage/source.tar" -C "$stage/source"
target="$stage/source/deploy/e2e/release-recovery/waltrace"
mkdir -p "$target"
for name in fixture_child.go trace_fixture.py test_trace_fixture.py run-local.sh README.md; do
    test -f "$source_dir/$name" && test ! -L "$source_dir/$name"
    cp -- "$source_dir/$name" "$target/$name"
    printf '%s\n' "deploy/e2e/release-recovery/waltrace/$name" >> "$stage/working-overlay.txt"
done
test -f "$source_dir/../wal_frames.py" && test ! -L "$source_dir/../wal_frames.py"
cp -- "$source_dir/../wal_frames.py" "$stage/source/deploy/e2e/release-recovery/wal_frames.py"
printf '%s\n' 'deploy/e2e/release-recovery/wal_frames.py' >> "$stage/working-overlay.txt"
cd "$stage/source"
find deploy/e2e/release-recovery/waltrace -type f ! -path '*/__pycache__/*' -print0 | sort -z | xargs -0 sha256sum > "$stage/fixture-source.sha256"
sha256sum deploy/e2e/release-recovery/wal_frames.py go.mod go.sum >> "$stage/fixture-source.sha256"
go_bin=${CELIKPANEL_WALTRACE_GO:-$(command -v go || true)}
if [ -z "$go_bin" ] || [ ! -x "$go_bin" ]; then
    printf '%s\n' 'Pinned Go 1.26.5 is not available; no toolchain is installed automatically.' > "$stage/build.log"
    exit 2
fi
go_bin=$(readlink -f -- "$go_bin")
export GOENV=off GOTOOLCHAIN=local GOWORK=off GOFLAGS= GOPROXY=off GOSUMDB=off
"$go_bin" version > "$stage/toolchain.txt"
if [ "$(head -n 1 "$stage/toolchain.txt")" != 'go version go1.26.5 linux/amd64' ]; then
    printf '%s\n' 'Expected exactly Go 1.26.5 linux/amd64; refusing a different toolchain.' >> "$stage/build.log"
    exit 2
fi
sha256sum "$go_bin" >> "$stage/toolchain.txt"
CGO_ENABLED=0 "$go_bin" build -mod=readonly -buildvcs=false -trimpath -o "$stage/fixture-child" ./deploy/e2e/release-recovery/waltrace/fixture_child.go > "$stage/build.log" 2>&1
"$go_bin" version -m "$stage/fixture-child" > "$stage/binary-modules.txt"
sha256sum "$stage/fixture-child" > "$stage/binary.sha256"
set +e
python3 deploy/e2e/release-recovery/waltrace/trace_fixture.py --fixture-binary "$stage/fixture-child" --output-directory "$stage/spill" > "$stage/trace.stdout" 2> "$stage/trace.stderr"
result=$?
set -e
printf '%s\n' "$result" > "$stage/trace.exit"
sha256sum --check "$stage/fixture-source.sha256" > "$stage/source-recheck.log"
cat "$stage/trace.stdout"
set +e
CELIKPANEL_WALTRACE_CHILD="$stage/fixture-child" CELIKPANEL_WALTRACE_EVIDENCE="$stage/test-evidence" python3 -m unittest discover -s deploy/e2e/release-recovery/waltrace -p 'test_*.py' -v > "$stage/tests.stdout" 2> "$stage/tests.stderr"
test_result=$?
set -e
printf '%s\n' "$test_result" > "$stage/tests.exit"
sha256sum --check "$stage/fixture-source.sha256" > "$stage/source-final-recheck.log"
cat "$stage/tests.stderr"
if [ "$result" -ne 0 ]; then exit "$result"; fi
exit "$test_result"
