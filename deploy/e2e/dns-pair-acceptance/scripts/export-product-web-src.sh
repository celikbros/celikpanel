#!/usr/bin/env bash
# Export one product commit's web/src next to an existing dist.json, for dist
# directories built before build-dist.sh exported it (for example pair2's
# 916e1577...-acceptance-license). New builds need no extra step.
#
# usage: export-product-web-src.sh COMMIT DIST_DIR
#   DIST_DIR: the directory holding dist.json; its "commit" must be COMMIT.
# Writes DIST_DIR/product-web-src/ (git archive COMMIT web/src, never the
# working tree) with a PRODUCT-COMMIT marker "<commit> <tree>". Refuses to reuse
# an existing export. dist.json is not changed; run-topology.sh finds the export
# next to it.
set -euo pipefail
umask 022

commit=${1:?usage: export-product-web-src.sh COMMIT DIST_DIR}
dir=${2:?usage: export-product-web-src.sh COMMIT DIST_DIR}
[[ $# -eq 2 ]] || { echo "usage: export-product-web-src.sh COMMIT DIST_DIR" >&2; exit 2; }
[[ $commit =~ ^[0-9a-f]{7,40}$ ]] || { echo "commit must be hex" >&2; exit 2; }
REPO=${CELIKPANEL_REPO:-'/mnt/c/CELIKBROS PROJECTS/celikpanel'}
full=$(git -c safe.directory='*' -C "$REPO" rev-parse "${commit}^{commit}")
tree=$(git -c safe.directory='*' -C "$REPO" rev-parse "${full}^{tree}")
[[ -f $dir/dist.json ]] || { echo "no dist.json in $dir" >&2; exit 2; }
recorded=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["commit"])' "$dir/dist.json")
[[ $recorded == "$full" ]] || { echo "dist.json is for $recorded, not $full" >&2; exit 2; }
out=$dir/product-web-src
[[ ! -e $out ]] || { echo "refusing to reuse $out" >&2; exit 2; }
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
git -c safe.directory='*' -C "$REPO" archive "$full" web/src | tar -x -C "$work"
[[ -f $work/web/src/components/ServerSetup.tsx ]] || { echo "commit $full has no web/src" >&2; exit 2; }
mkdir -p "$out"
cp -a "$work/web/src/." "$out/"
printf '%s %s\n' "$full" "$tree" > "$out/PRODUCT-COMMIT"
echo "EXPORTED $out"
