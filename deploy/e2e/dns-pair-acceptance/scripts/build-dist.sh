#!/usr/bin/env bash
# Build the customer install archive (make dist) for one exact commit, offline.
#
# usage: build-dist.sh [--acceptance-license] COMMIT
#
# Runs on the Linux QEMU host (the archlinux WSL distribution). Sources come
# from `git archive COMMIT` of the repository, never from the working tree.
# The frontend is taken from the repository's already built web/dist (build it
# for the same commit with `npm run build` in web/ first); make's web target is
# neutralised with NPM=true, exactly as the kill-matrix batches reuse web/dist.
# Output: /var/tmp/cp-pair-accept/dist/<commit>/ with the archive and dist.json.
#
# --acceptance-license (test only; driver --license-mode acceptance-fixture):
# first the ordinary make dist runs and must pass its own packaging checks,
# including the acceptance-license guard. Then a separate archive is derived
# with only bin/panel rebuilt with `-tags acceptance_license` (same flags), an
# ACCEPTANCE-LICENSE-BUILD.txt notice at its root and a regenerated SHA256SUMS.
# That panel accepts the acceptance fixture license, only on a disposable guest
# carrying the fixture's marker, and never contacts the license service. The
# ordinary dist recipe cannot produce it: make dist and the signed-release
# writer refuse it (deploy/release-acceptance-license-guard.sh). Output:
# /var/tmp/cp-pair-accept/dist/<commit>-acceptance-license/.
set -euo pipefail
umask 022

acceptance=0
if [[ ${1:-} == --acceptance-license ]]; then
    acceptance=1
    shift
fi
commit=${1:?usage: build-dist.sh [--acceptance-license] COMMIT}
[[ $# -eq 1 ]] || { echo "usage: build-dist.sh [--acceptance-license] COMMIT" >&2; exit 2; }
[[ $commit =~ ^[0-9a-f]{7,40}$ ]] || { echo "commit must be hex" >&2; exit 2; }
REPO=${CELIKPANEL_REPO:-'/mnt/c/CELIKBROS PROJECTS/celikpanel'}
GO=${CELIKPANEL_GO:-/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go}
command -v make >/dev/null || { echo "make is required" >&2; exit 2; }
[[ -x $GO ]] || { echo "reviewed Go toolchain missing: $GO" >&2; exit 2; }

full=$(git -c safe.directory='*' -C "$REPO" rev-parse "${commit}^{commit}")
tree=$(git -c safe.directory='*' -C "$REPO" rev-parse "${full}^{tree}")
epoch=$(git -c safe.directory='*' -C "$REPO" log -1 --format=%ct "$full")
version="v0.0.0-pairaccept.${full:0:12}"
out=/var/tmp/cp-pair-accept/dist/$full
if [[ $acceptance -eq 1 ]]; then
    # The version is visible in the panel (API /panel/version) and names the archive.
    version="v0.0.0-pairaccept-acceptance-license.${full:0:12}"
    out=/var/tmp/cp-pair-accept/dist/$full-acceptance-license
fi
src=$out/src
[[ ! -e $out ]] || { echo "refusing to reuse $out" >&2; exit 2; }
[[ -f $REPO/web/dist/index.html ]] || { echo "build web/dist for $full first" >&2; exit 2; }
mkdir -p "$src"
git -c safe.directory='*' -C "$REPO" archive "$full" | tar -x -C "$src"
mkdir -p "$src/web/dist"
cp -a "$REPO/web/dist/." "$src/web/dist/"
( cd "$src/web/dist" && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > "$out/web-dist.sha256"

export GOTOOLCHAIN=local CGO_ENABLED=0
make -C "$src" dist GO="$GO" NPM=true VERSION="$version" COMMIT="$full" TREE="$tree" \
    SOURCE_DATE_EPOCH="$epoch" > "$out/build.log" 2>&1
archive=$src/dist/celikpanel-$version.tar.gz
root=celikpanel-$version

if [[ $acceptance -eq 1 ]]; then
    [[ -f $src/internal/licensing/acceptance_fixture.go ]] \
        || { echo "commit $full has no acceptance license seam" >&2; exit 2; }
    guard=$src/deploy/release-acceptance-license-guard.sh
    # The ordinary archive passed make dist's guard; check it once more.
    bash "$guard" "$archive" || { echo "ordinary archive unexpectedly refused" >&2; exit 1; }
    work=$out/acceptance
    mkdir -p "$work"
    tar -tvzf "$archive" | awk '{ t = substr($1, 1, 1); if (t != "-" && t != "d") bad = 1 } END { exit (bad ? 1 : 0) }' \
        || { echo "ordinary archive holds links or special files" >&2; exit 1; }
    tar --no-same-owner -xzf "$archive" -C "$work"
    rm -f -- "$work/$root/bin/panel"
    ( cd "$src" && env -i HOME="$HOME" PATH="$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 \
        "$GO" build -tags acceptance_license -trimpath -buildvcs=false \
        -ldflags "-s -w -X main.buildVersion=$version -X main.buildCommit=$full" \
        -o "$work/$root/bin/panel" ./cmd/panel ) >> "$out/build.log" 2>&1
    chmod 0755 "$work/$root/bin/panel"
    "$GO" version -m "$work/$root/bin/panel" | grep -Fqx $'\tbuild\t-tags=acceptance_license' \
        || { echo "rebuilt panel does not record -tags=acceptance_license" >&2; exit 1; }
    cat > "$work/$root/ACCEPTANCE-LICENSE-BUILD.txt" <<EOF
This archive is NOT a CelikPanel release.

bin/panel was built from commit $full with the acceptance_license test tag
(deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh --acceptance-license).
It never contacts the license service and accepts only the acceptance fixture
license, only on a disposable acceptance guest carrying the fixture marker
/etc/celikpanel-dns-kill-matrix. Everything else is the ordinary make dist
output of the same commit. Release packaging refuses this archive.
Runs with it do not evidence license behaviour.
EOF
    chmod 0644 "$work/$root/ACCEPTANCE-LICENSE-BUILD.txt"
    # The guard must refuse exactly this tree: proof the refusal sees this panel.
    if bash "$guard" "$work/$root" > "$out/guard-refusal.txt" 2>&1; then
        echo "the acceptance-license guard did not refuse the acceptance build" >&2
        exit 1
    fi
    grep -Fq 'bin/panel was built with -tags acceptance_license' "$out/guard-refusal.txt" \
        || { echo "unexpected guard result; see $out/guard-refusal.txt" >&2; exit 1; }
    # write-release-manifest.sh refuses this tree by design, so SHA256SUMS is
    # regenerated here with the same rule (all files except itself, sorted).
    ( cd "$work/$root" && LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 | LC_ALL=C sort -z \
        | xargs -0 sha256sum > SHA256SUMS && sha256sum -c SHA256SUMS >/dev/null )
    archive=$out/celikpanel-$version-acceptance-license.tar.gz
    tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner --mode='u=rwX,go=rX' --format=gnu \
        -cf "${archive%.gz}" -C "$work" "$root"
    gzip -n -f "${archive%.gz}"
    rm -rf -- "$work"
else
    mv "$archive" "$out/"
    archive=$out/celikpanel-$version.tar.gz
fi
sha=$(sha256sum "$archive" | cut -d' ' -f1)
"$GO" version > "$out/go-version.txt"
license_mode=customer
[[ $acceptance -eq 0 ]] || license_mode=acceptance-fixture
python3 - "$out/dist.json" "$archive" "$sha" "$full" "$tree" "$version" "$license_mode" <<'PY'
import json, sys
path, archive, sha, commit, tree, version, license_mode = sys.argv[1:]
document = {"archive": archive, "sha256": sha, "commit": commit, "tree": tree,
            "version": version, "root": "celikpanel-" + version}
if license_mode == "acceptance-fixture":
    document.update({"license_mode": "acceptance-fixture", "panel_build_tags": "acceptance_license",
                     "release": False,
                     "note": "not a release; use only with pair_acceptance.py --license-mode acceptance-fixture"})
with open(path, "x", encoding="utf-8") as handle:
    json.dump(document, handle, indent=2, sort_keys=True)
    handle.write("\n")
PY
cat "$out/dist.json"
echo BUILD-OK
