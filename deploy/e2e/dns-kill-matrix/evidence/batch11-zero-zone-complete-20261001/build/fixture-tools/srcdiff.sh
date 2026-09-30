# Batch 11: redo the source-tree check against 542ccc8e (the first export compared with 0d4c0324 by a script error)
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
D="$REPO/deploy/e2e/dns-kill-matrix/evidence/batch11-zero-zone-complete-20261001"
test ! -e /var/tmp/cp-b11-verify
mkdir -p /var/tmp/cp-b11-verify && git -c safe.directory='*' -C "$REPO" archive 542ccc8e | tar -x -C /var/tmp/cp-b11-verify
{ echo "Source tree /root/cp-b11-src (built and tested) against a fresh git archive 542ccc8e, $(date -u +%FT%TZ). The first export compared against 0d4c0324 by a script error; this file replaces that output."; diff -r --exclude=dist /var/tmp/cp-b11-verify /root/cp-b11-src; echo "diff rc=$? (web/dist excluded: built from the archive's web/ during this run, not tracked)"; } > "$D/build/source-tree-vs-archive.diff"
rm -rf /var/tmp/cp-b11-verify
cat "$D/build/source-tree-vs-archive.diff" | head -20
