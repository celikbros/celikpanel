#!/bin/bash
# One re-run, as root, of the root-pass failures whose guest prerequisite was supplied.
set -u
out=/srv/rt/out/rerun; base=/srv/rt/rerun
rm -rf "$out" "$base"; mkdir -p "$out" "$base"
export PATH=/opt/celikpanel-test-toolchains/go1.26.5/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export GOTOOLCHAIN=local GOPROXY=off LANG=C.UTF-8 LC_ALL=C.UTF-8 HOME=/root
: > "$out/results.tsv"
runone() { # name dir
    local name=$1 dir=$2 t0 t1 rc
    t0=$(date +%s.%N)
    ( cd "$dir" && exec timeout -k 30 1800 bash "deploy/$name.sh" ) < /dev/null >> "$out/$name.log" 2>&1
    rc=$?
    t1=$(date +%s.%N)
    printf '%s\t%s\t%s\n' "$name" "$rc" "$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.1f", b-a}')" >> "$out/results.tsv"
}
fresh() { rm -rf "$base/$1"; mkdir -p "$base/$1"; tar -C "$base/$1" -xf /srv/rt/src.tar; }

# 1. dns-owner-tools-package: prerequisite 'make dns-owner-tools' (CI runs it in the same job first).
n=test-dns-owner-tools-package; fresh $n
{ echo "## prerequisite: make dns-owner-tools"; (cd "$base/$n" && make dns-owner-tools) 2>&1; echo "## make exit=$?"; } > "$out/$n.log"
runone $n "$base/$n"

# 2. release-sequence-policy: prerequisite git history with the pinned historical tag (CI: fetch-depth 0).
n=test-release-sequence-policy
{
  echo "## prerequisite: git clone of a bundle (candidate history + refs/tags/v0.1.0-alpha.79), detached at 48d264d5"
  git clone -q --no-checkout /root/celikpanel-release-recovery-lab/cand-48d264d5.bundle "$base/$n" 2>&1
  git -C "$base/$n" -c advice.detachedHead=false checkout -q --detach 48d264d5669099292deb64e7f90d267ac352b1ec 2>&1
  echo "HEAD=$(git -C "$base/$n" rev-parse HEAD) tree=$(git -C "$base/$n" rev-parse 'HEAD^{tree}')"
  echo "tags: $(git -C "$base/$n" tag | tr '\n' ' ')"
  git -C "$base/$n" archive --format=tar HEAD | sha256sum | sed 's/^/git-archive-in-guest sha256=/'
  echo "archive-under-test sha256=$(sha256sum < /srv/rt/src.tar)"
  git -C "$base/$n" status --porcelain | head -5
} > "$out/$n.log" 2>&1
runone $n "$base/$n"

# 3. Fixtures that execute files under /run: Debian 13 mounts /run noexec; allow exec for the re-run only.
findmnt -no OPTIONS /run > "$out/run-mount-before.txt"
mount -o remount,exec /run
findmnt -no OPTIONS /run > "$out/run-mount-during.txt"
for n in test-release-recovery-contract test-release-recovery-rollback-handoff test-rollback-material-admission; do
    fresh $n
    echo "## guest prerequisite: /run remounted exec ($(cat "$out/run-mount-during.txt"))" > "$out/$n.log"
    runone $n "$base/$n"
done
mount -o remount,noexec /run
findmnt -no OPTIONS /run > "$out/run-mount-after.txt"
rm -rf "$base"
echo DONE > "$out/DONE"
