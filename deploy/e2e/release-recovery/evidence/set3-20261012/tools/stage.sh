#!/bin/bash
# set3: stage one cell's evidence into the repository's evidence folder, verify the driver's SHA256SUMS on the staged
# copy, and only then remove THIS run's overlay disks of that lab (each listed in host/removals.txt).
# usage: stage.sh JOB LAB NODE CELL RUN [PREFIX]     (PREFIX: a sub-folder such as part2-alpha81)
set -euo pipefail
job=$1; lab=/var/tmp/cp-release-drill-$2; node=$3; cell=$4; run=$5; prefix=${6:-}
R=/var/tmp/cp-set3-run; L=$R/logs
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set3-20261012'
J=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3
case $2 in set3-*|rid3-*|u14-*) ;; *) echo "refusing: $2 is not a lab of this run"; exit 2;; esac
[ -e $L/$job.start ] || { echo "refusing: no job $job of this run"; exit 2; }
[ -e $L/$job.end ] || { echo "refusing: job $job has not ended"; exit 2; }
pgrep -f "cp-release-drill-$2/" > /dev/null && { echo "refusing: a process of lab $2 is still running"; pgrep -af "cp-release-drill-$2/" | cut -c1-120; exit 2; }
src=$(ls -d $lab/evidence/$node/upd1/${cell}-*/ | tail -1)
dst="$E/${prefix:+$prefix/}$cell/$run"
[ -e "$dst" ] && { echo "refusing: $dst exists"; exit 2; }
mkdir -p "$dst/host"
cp -r "$src". "$dst/"
for f in $lab/evidence/$node/*.json $lab/evidence/$node/*.log $lab/evidence/$node/worker-origin-manifest; do [ -f "$f" ] && cp "$f" "$dst/host/" || true; done
cp $lab/cells/*/fixture-plan.json "$dst/host/" 2>/dev/null || true
for x in start end rc out err; do cp $L/$job.$x "$dst/host/wrapper.$x.txt"; done
cp $J/job-${job}.sh "$dst/host/job.sh"
h=$(tr -d '\r\n' < $J/job-${job}.harness); o=${h/harness-/overlay-}
echo "harness=$R/$h (git archive cfa329676, overlay $o $(sha256sum $R/$o/harness.diff | cut -c1-16))" > "$dst/host/harness.txt"
{ echo "lab=$lab"; basename "$src"; } > "$dst/host/lab.txt"
if [ -f "$dst/SHA256SUMS" ]; then
  ( cd "$dst" && sha256sum -c --quiet SHA256SUMS ) && echo "driver SHA256SUMS verified on the staged copy ($(wc -l < "$dst/SHA256SUMS") files)" || { echo "SHA256SUMS FAILED"; exit 3; }
else
  # a run that was stopped before its result: every staged file is compared with its source one by one
  ( cd "$src" && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > "$dst/host/staged-files.sha256"
  ( cd "$dst" && sha256sum -c --quiet host/staged-files.sha256 ) && echo "no driver SHA256SUMS (run without a result); $(wc -l < "$dst/host/staged-files.sha256") staged files equal their source" || { echo "STAGED COPY DIFFERS"; exit 3; }
fi
mkdir -p "$E/host"
for d in $lab/cells/*/*/overlay.qcow2; do
  [ -f "$d" ] || continue
  b=$(stat -c %s "$d"); rm -f -- "$d"
  echo "$(date -u +%FT%TZ) removed $d bytes=$b (evidence staged at set3-20261012/${prefix:+$prefix/}$cell/$run, checksums verified)" | tee -a "$E/host/removals.txt"
done
find "$dst" -type f | wc -l
python3 - "$dst/result.json" <<'PY'
import json, sys
try:
    r = json.load(open(sys.argv[1]))
except Exception as exc:
    print("no result.json:", type(exc).__name__); raise SystemExit(0)
print("overall:", r.get("overall"), "| native_evidence:", r.get("native_evidence"), "| outcome:", (r.get("outcome") or {}).get("classification"))
for s in r.get("steps", []):
    if s.get("verdict") not in ("passed", "observed"):
        print("  step", s.get("name"), s.get("verdict"), "|", str(s.get("reason"))[:700])
for k, v in (r.get("sections") or {}).items():
    if v.get("verdict") != "passed":
        print("  section", k, v.get("verdict"), "| failed:", [f[:160] for f in (v.get("failed") or [])][:8], "| unknown:", [u[:120] for u in (v.get("unknown") or [])][:4], "| error:", str(v.get("error"))[:300])
PY
