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
# Also written next to the archive (pair2 corrections):
# - product-web-src/: the commit's web/src with a PRODUCT-COMMIT marker
#   ("<commit> <tree>"). The driver resolves every guidance text from it
#   (--product-web-src; run-topology.sh passes it from dist.json), so the texts
#   are the PRODUCT's even when the driver comes from another commit.
# - archive-evidence-entries.json: how many archive entries lie under any
#   evidence/ directory and under deploy/e2e/ (a recorded observation only;
#   nothing is refused on it). The counts are also in dist.json.
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
# Optional exact label for a disposable fixture commit whose committed release
# policy names that version (release-recovery upd1: v0.1.0-alpha.81/82). The
# default labels above are unchanged; make dist still checks policy/version.
if [[ -n ${CELIKPANEL_DIST_VERSION:-} ]]; then
    [[ $CELIKPANEL_DIST_VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$ ]] \
        || { echo "CELIKPANEL_DIST_VERSION must look like v0.1.0-alpha.N" >&2; exit 2; }
    version=$CELIKPANEL_DIST_VERSION
fi
src=$out/src
[[ ! -e $out ]] || { echo "refusing to reuse $out" >&2; exit 2; }
[[ -f $REPO/web/dist/index.html ]] || { echo "build web/dist for $full first" >&2; exit 2; }
mkdir -p "$src"
git -c safe.directory='*' -C "$REPO" archive "$full" | tar -x -C "$src"
# The product's own texts for the driver, exported before anything is built.
[[ -f $src/web/src/components/ServerSetup.tsx ]] || { echo "commit $full has no web/src" >&2; exit 2; }
mkdir -p "$out/product-web-src"
cp -a "$src/web/src/." "$out/product-web-src/"
printf '%s %s\n' "$full" "$tree" > "$out/product-web-src/PRODUCT-COMMIT"
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
    # upd7: a published tag that predates the guard (v0.1.0-alpha.80) is checked with the
    # guard named by CELIKPANEL_ACCEPTANCE_GUARD (the harness commit's own copy); the tag's
    # make dist does not run it, and the tag's deploy/ tree is not changed to add it.
    if [[ ! -f $guard && -n ${CELIKPANEL_ACCEPTANCE_GUARD:-} ]]; then
        [[ -f $CELIKPANEL_ACCEPTANCE_GUARD ]] || { echo "CELIKPANEL_ACCEPTANCE_GUARD is not a file" >&2; exit 2; }
        guard=$CELIKPANEL_ACCEPTANCE_GUARD
        echo "acceptance guard: $guard (the commit has none)" >> "$out/build.log"
    fi
    [[ -f $guard ]] || { echo "commit $full has no acceptance license guard" >&2; exit 2; }
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
# Observation only (pair2 operator did this by hand): archive entries under any
# evidence/ directory and under deploy/e2e/. The product is fixed separately.
python3 - "$archive" "$sha" > "$out/archive-evidence-entries.json" <<'PY'
import json, sys, tarfile
archive, sha = sys.argv[1:]
total = 0
groups = {"under_evidence_dir": [], "under_deploy_e2e": []}
with tarfile.open(archive, "r:gz") as bundle:
    for member in bundle:
        total += 1
        name = member.name.lstrip("./").rstrip("/")
        relative = name.split("/", 1)[1] if "/" in name else ""
        parts = relative.split("/") if relative else []
        kind = "file" if member.isfile() else "dir" if member.isdir() else "other"
        if "evidence" in parts[:-1]:
            groups["under_evidence_dir"].append((relative, kind))
        if relative.startswith("deploy/e2e/"):
            groups["under_deploy_e2e"].append((relative, kind))
document = {
    "schema": "celikpanel/dns-pair-acceptance-archive-observation/v1",
    "archive": archive,
    "sha256": sha,
    "total_entries": total,
    "note": "recorded observation only; entries are counted, nothing is refused on them",
}
for key, entries in groups.items():
    document[key] = {
        "entries": len(entries),
        "files": sum(1 for _, kind in entries if kind == "file"),
        "directories": sum(1 for _, kind in entries if kind == "dir"),
        "sample": sorted(path for path, _ in entries)[:10],
    }
json.dump(document, sys.stdout, indent=2, sort_keys=True)
sys.stdout.write("\n")
PY
license_mode=customer
[[ $acceptance -eq 0 ]] || license_mode=acceptance-fixture
python3 - "$out/dist.json" "$archive" "$sha" "$full" "$tree" "$version" "$license_mode" "$out" <<'PY'
import json, sys
path, archive, sha, commit, tree, version, license_mode, out = sys.argv[1:]
with open(out + "/archive-evidence-entries.json", encoding="utf-8") as handle:
    observed = json.load(handle)
document = {"archive": archive, "sha256": sha, "commit": commit, "tree": tree,
            "version": version, "root": "celikpanel-" + version,
            "product_web_src": out + "/product-web-src", "product_web_src_commit": commit,
            "archive_observation": {
                "file": out + "/archive-evidence-entries.json",
                "total_entries": observed["total_entries"],
                "entries_under_evidence_dirs": observed["under_evidence_dir"]["entries"],
                "entries_under_deploy_e2e": observed["under_deploy_e2e"]["entries"],
                "note": observed["note"]}}
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
