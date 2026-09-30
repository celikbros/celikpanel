#!/usr/bin/env bash
# Build the five upd1/upd3 archives from one exact source commit, in a disposable clone.
#
# usage: build-upd1-artifacts.sh [SOURCE_COMMIT]      (default: the repository HEAD)
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
SOURCE=${1:-HEAD}
BUILD_DIST="$HERE/../dns-pair-acceptance/scripts/build-dist.sh"
DRIVER="$HERE/owner_update_trial.py"
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
    python3 "$DRIVER" fixture-source --repo "$clone" --kind "$kind" ${previous:+--previous-commit "$previous"} >&2
    git -C "$clone" commit --quiet -am "$message"
    commit=$(git -C "$clone" rev-parse HEAD)
    # Sibling fixture commits leave HEAD; a ref keeps each one reachable in this clone.
    git -C "$clone" update-ref "refs/upd1/$kind" "$commit"
    echo "$commit"
}
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

build() {
    local commit=$1 version=$2
    CELIKPANEL_REPO=$clone CELIKPANEL_DIST_VERSION=$version bash "$BUILD_DIST" --acceptance-license "$commit" >&2
    echo "/var/tmp/cp-pair-accept/dist/$commit-acceptance-license/dist.json"
}
b_json=$(build "$baseline" v0.1.0-alpha.81)
g_json=$(build "$good" v0.1.0-alpha.82)
d_json=$(build "$defective" v0.1.0-alpha.82)
s_json=$(build "$startcheck" v0.1.0-alpha.82)
r_json=$(build "$realstart" v0.1.0-alpha.82)

python3 - "$work/upd1-artifacts.json" "$source_commit" "$clone" "$b_json" "$g_json" "$d_json" \
    "$baseline" "$good" "$s_json" "$r_json" <<'PY'
import json, sys
out, head, clone, b, g, d, baseline, good, s, r = sys.argv[1:]
def item(path, sequence, parent=None):
    value = json.load(open(path))
    entry = {k: value[k] for k in ("archive", "sha256", "commit", "tree", "version", "product_web_src")}
    entry.update(sequence=sequence, license_mode=value.get("license_mode"), dist_json=path)
    if parent:
        entry["parent"] = parent
    return entry
document = {"schema": "celikpanel/upd1-artifacts/v1", "source_head": head, "clone": clone,
            "baseline": item(b, 81), "good": item(g, 82, baseline), "defective": item(d, 82, good),
            "startcheck": item(s, 82, good), "realstart": item(r, 82, good),
            "provenance": "unpublished disposable fixture commits over the source HEAD; acceptance-license panel "
                          "(D-027 fixture); signed only by the per-lab fixture key at run time; not a release"}
document["defective"]["defect"] = "cmd/panel --migrate-only exits 1 after migrating the isolated copy"
document["startcheck"]["defect"] = ("cmd/panel configurePanelHTTPTLS (shared by --check-startup-readiness and the "
                                    "real start) always fails")
document["realstart"]["defect"] = "cmd/panel main() exits before its listener; the start check never reaches it"
with open(out, "x") as handle:
    json.dump(document, handle, indent=2, sort_keys=True)
    handle.write("\n")
print(out)
PY
echo UPD1-ARTIFACTS-OK
