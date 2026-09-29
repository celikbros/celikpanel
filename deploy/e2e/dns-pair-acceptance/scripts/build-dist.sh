#!/usr/bin/env bash
# Build the customer install archive (make dist) for one exact commit, offline.
#
# usage: build-dist.sh COMMIT
#
# Runs on the Linux QEMU host (the archlinux WSL distribution). Sources come
# from `git archive COMMIT` of the repository, never from the working tree.
# The frontend is taken from the repository's already built web/dist (build it
# for the same commit with `npm run build` in web/ first); make's web target is
# neutralised with NPM=true, exactly as the kill-matrix batches reuse web/dist.
# Output: /var/tmp/cp-pair-accept/dist/<commit>/ with the archive and dist.json.
set -euo pipefail
umask 022

commit=${1:?usage: build-dist.sh COMMIT}
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
mv "$archive" "$out/"
archive=$out/celikpanel-$version.tar.gz
sha=$(sha256sum "$archive" | cut -d' ' -f1)
"$GO" version > "$out/go-version.txt"
python3 - "$out/dist.json" "$archive" "$sha" "$full" "$tree" "$version" <<'PY'
import json, sys
path, archive, sha, commit, tree, version = sys.argv[1:]
with open(path, "x", encoding="utf-8") as handle:
    json.dump({"archive": archive, "sha256": sha, "commit": commit, "tree": tree,
               "version": version, "root": "celikpanel-" + version}, handle, indent=2, sort_keys=True)
    handle.write("\n")
PY
cat "$out/dist.json"
echo BUILD-OK
