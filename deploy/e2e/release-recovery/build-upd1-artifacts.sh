#!/usr/bin/env bash
# Build the five upd1/upd3 archives from one exact source commit, in a disposable clone.
#
# usage: build-upd1-artifacts.sh [--baseline-ref TAG] [SOURCE_COMMIT]   (default: the repository HEAD)
#
# Runs on the Linux QEMU host (archlinux), as the pair driver's build does.
# Nothing is committed to the working repository: the fixture commits exist
# only in a new clone under /var/tmp/cp-upd1-build/<stamp>/repo, each kept
# reachable by a refs/upd1/<kind> ref of that clone.
#
#   B  baseline    SOURCE + release policy v0.1.0-alpha.81 / 81 (previous Alpha80)
#   G  good        B + release policy v0.1.0-alpha.82 / 82 (previous = B)
#   D  defective   G + fixture defect: cmd/panel --migrate-only exits 1 after
#                  migrating the isolated copy (owner_update_trial.apply_defect)
#   S  startcheck  G + fixture defect: configurePanelHTTPTLS, shared by the
#                  read-only start check and the real start, always fails
#                  (owner_update_trial.apply_kind_patch start-check)
#   R  realstart   G + fixture defect: main() exits just before the listener;
#                  the start check never reaches it (apply_kind_patch real-start)
#
# --baseline-ref TAG (upd7; only v0.1.0-alpha.80): the baseline is the PUBLISHED
# tag's own tree plus the D-027 acceptance-license seam (Panel license code
# only: owner_update_trial.BASELINE_REF_PATCHED), labelled by the tag's own
# unchanged release policy. The build refuses unless `git diff TAG B` names
# exactly those files, and writes baseline-ref-proof.txt (blob and SHA-256 of
# every update/rollback/recovery/bootstrap/get.sh/deploy/release-*/finalize-*
# file at TAG and at B, the cmd/agent, deploy/, download-portal/ and web/
# trees, and the Agent's package closure, which never includes the seam).
# The candidates are SOURCE labelled as the next release (v0.1.0-alpha.81 / 81,
# previous = B): G, and D over G. S and R are not built in this mode. B's web/
# is built from the tag, G's and D's from SOURCE.
#
# The source commit must carry the candidate start check (product 8ffc5e06 or
# later); the fixture patches refuse any other source text.
#
# Each commit is built with dns-pair-acceptance/scripts/build-dist.sh
# --acceptance-license (D-027 fixture license; the archive carries its
# ACCEPTANCE-LICENSE-BUILD.txt notice) and CELIKPANEL_DIST_VERSION set to the
# committed policy's version. Output: <stamp>/upd1-artifacts.json for
# owner_update_trial.py. None of these archives is a release.
set -euo pipefail
umask 022

HERE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO=${CELIKPANEL_REPO:-'/mnt/c/CELIKBROS PROJECTS/celikpanel'}
BASELINE_REF=
if [[ ${1:-} == --baseline-ref ]]; then
    BASELINE_REF=${2:?usage: build-upd1-artifacts.sh [--baseline-ref TAG] [SOURCE_COMMIT]}
    shift 2
    [[ $BASELINE_REF == v0.1.0-alpha.80 ]] || { echo "only --baseline-ref v0.1.0-alpha.80 is supported" >&2; exit 2; }
fi
SOURCE=${1:-HEAD}
BUILD_DIST="$HERE/../dns-pair-acceptance/scripts/build-dist.sh"
DRIVER="$HERE/owner_update_trial.py"
GUARD="$HERE/../../../deploy/release-acceptance-license-guard.sh"
GO=${CELIKPANEL_GO:-/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go}
[[ -f $BUILD_DIST && -f $DRIVER ]] || { echo "harness files missing" >&2; exit 2; }
command -v git >/dev/null && command -v python3 >/dev/null || { echo "git and python3 are required" >&2; exit 2; }

source_commit=$(git -c safe.directory='*' -C "$REPO" rev-parse "${SOURCE}^{commit}")
stamp=$(date -u +%Y%m%dt%H%M%Sz)
work=/var/tmp/cp-upd1-build/$stamp
[[ ! -e $work ]] || { echo "refusing to reuse $work" >&2; exit 2; }
mkdir -p "$work"
clone=$work/repo
git -c safe.directory='*' clone --quiet --no-hardlinks "$REPO" "$clone"
git -C "$clone" checkout --quiet --detach "$source_commit"
git -C "$clone" config user.name "upd1 disposable fixture"
git -C "$clone" config user.email "upd1-fixture@example.invalid"

commit_fixture() {
    local kind=$1 message=$2 previous=${3:-} commit
    python3 "$DRIVER" fixture-source --repo "$clone" --kind "$kind" ${previous:+--previous-commit "$previous"} \
        ${BASELINE_REF:+--baseline-ref "$BASELINE_REF"} >&2
    # set1 H23: once the source carries the baseline label itself (the release policy of a source after the
    # v0.1.0-alpha.81 release already is "v0.1.0-alpha.81 / 81"), the baseline fixture edit changes nothing;
    # the fixture commit is then an empty one over the source (same tree), instead of a failed commit.
    git -C "$clone" commit --quiet --allow-empty -am "$message"
    commit=$(git -C "$clone" rev-parse HEAD)
    # Sibling fixture commits leave HEAD; a ref keeps each one reachable in this clone.
    git -C "$clone" update-ref "refs/upd1/$kind" "$commit"
    echo "$commit"
}

build_web() {   # build web/ of the checked-out tree into $1
    ( cd "$clone/web" && rm -rf dist && npm ci --no-audit --no-fund && npm run build ) >&2
    [[ -f $clone/web/dist/index.html ]] || { echo "web build produced no index.html" >&2; exit 1; }
    rm -rf -- "$1"
    cp -a "$clone/web/dist" "$1"
}

use_web() {     # place one saved web/dist into the clone for build-dist.sh
    rm -rf -- "$clone/web/dist"
    cp -a "$1" "$clone/web/dist"
}

build() {
    local commit=$1 version=$2
    CELIKPANEL_REPO=$clone CELIKPANEL_DIST_VERSION=$version CELIKPANEL_ACCEPTANCE_GUARD=$GUARD \
        bash "$BUILD_DIST" --acceptance-license "$commit" >&2
    echo "/var/tmp/cp-pair-accept/dist/$commit-acceptance-license/dist.json"
}

if [[ -n $BASELINE_REF ]]; then
    tag_commit=$(git -C "$clone" rev-parse "${BASELINE_REF}^{commit}")
    git -C "$clone" checkout --quiet --detach "$tag_commit"
    python3 "$DRIVER" fixture-source --repo "$clone" --kind baseline-ref --baseline-ref "$BASELINE_REF" \
        --source-commit "$source_commit" >&2
    git -C "$clone" add -A -- internal/licensing cmd/panel/license.go
    git -C "$clone" commit --quiet -m "test(fixture): upd7 baseline = published $BASELINE_REF + the D-027 acceptance-license seam only (disposable)"
    baseline=$(git -C "$clone" rev-parse HEAD)
    git -C "$clone" update-ref refs/upd1/baseline "$baseline"
    expected=$(python3 -c 'import importlib.util,sys;s=importlib.util.spec_from_file_location("o",sys.argv[1]);m=importlib.util.module_from_spec(s);sys.modules["o"]=m;s.loader.exec_module(m);print("\n".join(m.BASELINE_REF_PATCHED))' "$DRIVER")
    actual=$(git -C "$clone" diff --name-only "$tag_commit" "$baseline" | LC_ALL=C sort)
    [[ $actual == "$expected" ]] || { echo "baseline fixture changed other files than the seam: $actual" >&2; exit 1; }
    proof=$work/baseline-ref-proof.txt
    {
        echo "baseline-ref $BASELINE_REF tag_commit=$tag_commit baseline_fixture=$baseline seam_source=$source_commit"
        echo "== git diff --name-status tag..baseline"
        git -C "$clone" diff --name-status "$tag_commit" "$baseline"
        echo "== trees (tag, baseline)"
        for t in cmd/agent deploy download-portal web internal/transport; do
            a=$(git -C "$clone" rev-parse "$tag_commit:$t") b=$(git -C "$clone" rev-parse "$baseline:$t")
            echo "$t $a $b $([[ $a == "$b" ]] && echo identical || echo DIFFERENT)"
        done
        echo "== files (path, tag blob, baseline blob, sha256 at tag, sha256 at baseline)"
        git -C "$clone" ls-tree -r --name-only "$tag_commit" -- update.sh rollback.sh install.sh rebuild.sh \
            bootstrap-update.sh bootstrap-prebuilt-update.sh download-portal/get.sh deploy \
            | grep -E '^(update\.sh|rollback\.sh|install\.sh|rebuild\.sh|bootstrap-[^/]*\.sh|download-portal/get\.sh|deploy/release-[^/]*|deploy/finalize-[^/]*|deploy/systemd/.*|deploy/enroll-signed-release-trust\.sh)$' \
            | while read -r path; do
                a=$(git -C "$clone" rev-parse "$tag_commit:$path") b=$(git -C "$clone" rev-parse "$baseline:$path")
                sa=$(git -C "$clone" cat-file blob "$a" | sha256sum | cut -d' ' -f1)
                sb=$(git -C "$clone" cat-file blob "$b" | sha256sum | cut -d' ' -f1)
                echo "$path $a $b $sa $sb $([[ $a == "$b" && $sa == "$sb" ]] && echo identical || echo DIFFERENT)"
            done
        ( cd "$clone" && GOTOOLCHAIN=local GOFLAGS=-mod=readonly "$GO" list -deps ./cmd/agent ) > "$work/agent-deps.txt"
        echo "== Agent package closure at the baseline (go list -deps ./cmd/agent, $(wc -l < "$work/agent-deps.txt") packages): licensing packages"
        grep -c '/internal/licensing' "$work/agent-deps.txt" || true
    } > "$proof"
    ! grep -q DIFFERENT "$proof" || { echo "a byte-identical baseline file differs from the tag; see $proof" >&2; exit 1; }
    [[ $(tail -n 1 "$proof") == 0 ]] || { echo "the Agent package closure includes the licensing seam; see $proof" >&2; exit 1; }
    build_web "$work/web-dist-baseline"
    git -C "$clone" checkout --quiet --detach "$source_commit"
    good=$(commit_fixture good "test(fixture): upd7 good candidate labelled v0.1.0-alpha.81 after the published $BASELINE_REF (unpublished, disposable)" "$baseline")
    defective=$(commit_fixture defective "test(fixture): upd7 defective candidate - migrate-only fails (unpublished, disposable)")
    build_web "$work/web-dist-candidate"
    [[ -z $(git -C "$clone" diff --stat "$source_commit" "$defective" -- web) ]] || { echo "fixture commit changed web/" >&2; exit 1; }
    use_web "$work/web-dist-baseline"
    b_json=$(build "$baseline" "$BASELINE_REF")
    use_web "$work/web-dist-candidate"
    g_json=$(build "$good" v0.1.0-alpha.81)
    d_json=$(build "$defective" v0.1.0-alpha.81)
    s_json= r_json= startcheck= realstart=
    b_seq=80 c_seq=81 b_parent=$tag_commit
else
baseline=$(commit_fixture baseline "test(fixture): upd1 baseline labelled v0.1.0-alpha.81 (unpublished, disposable)")
good=$(commit_fixture good "test(fixture): upd1 good candidate labelled v0.1.0-alpha.82 (unpublished, disposable)" "$baseline")
defective=$(commit_fixture defective "test(fixture): upd1 defective candidate - migrate-only fails (unpublished, disposable)")
# upd3: each start kind is one commit over G (never over D).
git -C "$clone" checkout --quiet --detach "$good"
startcheck=$(commit_fixture start-check "test(fixture): upd3 start-check candidate - shared panel TLS preparation fails (unpublished, disposable)")
git -C "$clone" checkout --quiet --detach "$good"
realstart=$(commit_fixture real-start "test(fixture): upd3 real-start candidate - exits before its listener (unpublished, disposable)")
git -C "$clone" checkout --quiet --detach "$defective"

# One frontend build serves all three commits: the fixture commits do not touch web/.
# Default: build it fresh from the source commit in the clone. An explicit
# CELIKPANEL_UPD1_WEB_DIST must be a web/dist built from that same commit.
if [[ -n ${CELIKPANEL_UPD1_WEB_DIST:-} ]]; then
    [[ -f $CELIKPANEL_UPD1_WEB_DIST/index.html && -f $CELIKPANEL_UPD1_WEB_DIST/recovery-offline.html ]] \
        || { echo "CELIKPANEL_UPD1_WEB_DIST is not a complete web/dist" >&2; exit 2; }
    mkdir -p "$clone/web/dist"
    cp -a "$CELIKPANEL_UPD1_WEB_DIST/." "$clone/web/dist/"
else
    ( cd "$clone/web" && npm ci --no-audit --no-fund && npm run build ) >&2
fi
for c in "$good" "$defective" "$startcheck" "$realstart"; do
    [[ -z $(git -C "$clone" diff --stat "$baseline" "$c" -- web) ]] || { echo "fixture commit changed web/" >&2; exit 1; }
done
# Each start kind changes exactly its one reviewed file over G.
[[ $(git -C "$clone" diff --name-only "$good" "$startcheck") == cmd/panel/server_lifecycle.go ]] \
    || { echo "start-check fixture changed more than cmd/panel/server_lifecycle.go" >&2; exit 1; }
[[ $(git -C "$clone" diff --name-only "$good" "$realstart") == cmd/panel/main.go ]] \
    || { echo "real-start fixture changed more than cmd/panel/main.go" >&2; exit 1; }

b_json=$(build "$baseline" v0.1.0-alpha.81)
g_json=$(build "$good" v0.1.0-alpha.82)
d_json=$(build "$defective" v0.1.0-alpha.82)
s_json=$(build "$startcheck" v0.1.0-alpha.82)
r_json=$(build "$realstart" v0.1.0-alpha.82)
b_seq=81 c_seq=82 b_parent= tag_commit=
fi

python3 - "$work/upd1-artifacts.json" "$source_commit" "$clone" "$b_json" "$g_json" "$d_json" \
    "$baseline" "$good" "$s_json" "$r_json" "$b_seq" "$c_seq" "$b_parent" "$BASELINE_REF" "$tag_commit" "$DRIVER" \
    "$work/baseline-ref-proof.txt" <<'PY'
import importlib.util, json, sys
out, head, clone, b, g, d, baseline, good, s, r, b_seq, c_seq, b_parent, ref, tag, driver, proof = sys.argv[1:]
b_seq, c_seq = int(b_seq), int(c_seq)
def item(path, sequence, parent=None):
    value = json.load(open(path))
    entry = {k: value[k] for k in ("archive", "sha256", "commit", "tree", "version", "product_web_src")}
    entry.update(sequence=sequence, license_mode=value.get("license_mode"), dist_json=path)
    if parent:
        entry["parent"] = parent
    return entry
document = {"schema": "celikpanel/upd1-artifacts/v1", "source_head": head, "clone": clone,
            "baseline": item(b, b_seq, b_parent or None), "good": item(g, c_seq, baseline),
            "defective": item(d, c_seq, good),
            "provenance": "unpublished disposable fixture commits over the source HEAD; acceptance-license panel "
                          "(D-027 fixture); signed only by the per-lab fixture key at run time; not a release"}
if s:
    document["startcheck"] = item(s, c_seq, good)
    document["startcheck"]["defect"] = ("cmd/panel configurePanelHTTPTLS (shared by --check-startup-readiness and "
                                        "the real start) always fails")
if r:
    document["realstart"] = item(r, c_seq, good)
    document["realstart"]["defect"] = "cmd/panel main() exits before its listener; the start check never reaches it"
document["defective"]["defect"] = "cmd/panel --migrate-only exits 1 after migrating the isolated copy"
if ref:
    spec = importlib.util.spec_from_file_location("upd7_driver", driver)
    module = importlib.util.module_from_spec(spec); sys.modules["upd7_driver"] = module; spec.loader.exec_module(module)
    document["baseline_ref"] = {"ref": ref, "tag_commit": tag, "patched_files": list(module.BASELINE_REF_PATCHED),
                                "proof": proof}
    document["provenance"] = (f"baseline: the published {ref} tree ({tag}) plus the D-027 acceptance-license seam only; "
                              "candidates: unpublished disposable fixture commits over the source labelled "
                              "v0.1.0-alpha.81; acceptance-license panels; signed only by the per-lab fixture key at "
                              "run time; not a release")
with open(out, "x") as handle:
    json.dump(document, handle, indent=2, sort_keys=True)
    handle.write("\n")
print(out)
PY
echo UPD1-ARTIFACTS-OK
