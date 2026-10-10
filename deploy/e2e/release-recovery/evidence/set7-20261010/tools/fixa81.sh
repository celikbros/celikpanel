#!/bin/bash
# set7: the a81 build's baseline is the published v0.1.0-alpha.81 tag itself; a full diff of 2a0af8866 against it
# (186 MB: the whole alpha.81 -> alpha.82 change) says nothing about the fixture. Kept instead: the tag check and counts.
set -eu
E="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set7-20261010/build/a81"
B=$(dirname "$(cat /var/tmp/cp-set7-run/artifacts-a81.path)")
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
G() { git -c safe.directory='*' "$@"; }
base=$(python3 -I -c "import json,sys;print(json.load(open(sys.argv[1]))['baseline']['commit'])" "$(cat /var/tmp/cp-set7-run/artifacts-a81.path)")
good=$(python3 -I -c "import json,sys;print(json.load(open(sys.argv[1]))['good']['commit'])" "$(cat /var/tmp/cp-set7-run/artifacts-a81.path)")
{
  echo "baseline commit $base; v0.1.0-alpha.81 tag commit $(G -C "$REPO" rev-parse 'v0.1.0-alpha.81^{commit}'); equal: $([ "$base" = "$(G -C "$REPO" rev-parse 'v0.1.0-alpha.81^{commit}')" ] && echo yes || echo NO)"
  echo "git diff --stat 2a0af8866 $base (the whole alpha.82 -> alpha.81 difference, not a fixture): $(G -C $B/repo diff --stat 2a0af8866 $base | tail -n 1)"
} > "$E/fixture-patch-baseline.txt"
rm -f -- "$E/fixture-patch-baseline.diff"
python3 -I - "$E/trees.txt" <<'PY'
import sys
lines = open(sys.argv[1]).read().splitlines()
out, skipping, n = [], False, 0
for line in lines:
    if line.startswith("### files that differ between the source 2a0af8866 and the baseline"):
        out.append(line + " (list omitted: the baseline is the published tag; see fixture-patch-baseline.txt)"); skipping = True; continue
    if skipping and line.startswith("###"):
        out.append(f"({n} entries omitted)"); skipping = False
    if skipping:
        n += 1; continue
    out.append(line)
open(sys.argv[1], "w").write("\n".join(out) + "\n")
PY
du -sh "$E"; cat "$E/fixture-patch-baseline.txt"; tail -n 6 "$E/trees.txt"
