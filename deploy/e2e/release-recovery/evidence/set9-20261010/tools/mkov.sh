#!/bin/bash
# set9: a run copy = git archive 7c3a05809 (branch fix/alpha83-known-state-gates; = e508af230 + a documentation commit) WITHOUT the retained-evidence folders of earlier runs
# (deploy/e2e/{release-recovery,dns-kill-matrix,dns-pair-acceptance}/evidence: about 490 MB of records, no harness
# code; one small folder is kept, see below) + exactly the listed harness files from the working tree (CRLF normalized to LF), each listed with its
# SHA-256, and the diff against 2a0af8866 (a new file diffs against /dev/null).
# usage: mkov.sh NAME [FILE...]   (FILE relative to deploy/e2e/release-recovery) -> $R/harness-NAME, $R/overlay-NAME
set -euo pipefail
R=/var/tmp/cp-set9-run
C=7c3a05809
name=$1; shift
H=$R/harness-$name
O=$R/overlay-$name
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
G() { git -c safe.directory='*' -C "$REPO" "$@"; }
[ -e $H ] && { echo "refusing: $H exists"; exit 2; }
EXTRA_EXCLUDE=${SET6_EXTRA_EXCLUDE:-}
mkdir -p $H $O $R/logs
# Kept of the earlier evidence: release-recovery/evidence/upd1-20261001 only (6 MB), because four offline tests of
# test_owner_update_trial read fixtures from it (copy `a` was made without it and shows exactly those four errors).
G archive $C | tar -x -C $H --exclude=deploy/e2e/dns-kill-matrix/evidence --exclude=deploy/e2e/dns-pair-acceptance/evidence   --exclude='deploy/e2e/release-recovery/evidence/set*' --exclude='deploy/e2e/release-recovery/evidence/roottests*'   --exclude='deploy/e2e/release-recovery/evidence/upd[2-9]*' --exclude='deploy/e2e/release-recovery/evidence/upd1[0-9]*' $EXTRA_EXCLUDE
( cd $H && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > $O/pristine-files.sha256
# every file of the pristine copy against the commit's own blob (git's object id of the file's bytes)
( cd $H && find . -type f | sort | sed 's#^\./##' | while IFS= read -r p; do echo "$(git hash-object -- "$p") $p"; done ) | sort > $O/runcopy-blob-list.txt
G ls-tree -r $C | awk '{id=$3; sub(/^[^	]*	/, ""); print id " " $0}' | sort > $O/commit-blob-list.txt
{
  echo "run copy $name (pristine, before the overlay) against commit $(G rev-parse "$C^{commit}") tree $(G rev-parse "$C^{tree}")"
  echo "files in the copy: $(wc -l < $O/runcopy-blob-list.txt)"
  echo "files of the copy whose blob id and path are NOT in the commit: $(comm -23 $O/runcopy-blob-list.txt $O/commit-blob-list.txt | wc -l)"
  echo "files of the commit not in the copy: $(comm -13 $O/runcopy-blob-list.txt $O/commit-blob-list.txt | wc -l)"
  echo "of those, outside deploy/e2e/{release-recovery,dns-kill-matrix,dns-pair-acceptance}/evidence/: $(comm -13 $O/runcopy-blob-list.txt $O/commit-blob-list.txt | grep -v -c -E ' deploy/e2e/(release-recovery|dns-kill-matrix|dns-pair-acceptance)/evidence/' || true)"
  echo "files of the copy under deploy/e2e/release-recovery/evidence/: $(grep -c ' deploy/e2e/release-recovery/evidence/' $O/runcopy-blob-list.txt || true) (all under upd1-20261001: $(grep -c ' deploy/e2e/release-recovery/evidence/upd1-20261001/' $O/runcopy-blob-list.txt || true))"
} > $O/runcopy-against-commit.txt
cat $O/runcopy-against-commit.txt
: > $O/files.sha256; : > $O/harness.diff
for b in "$@"; do
  f=deploy/e2e/release-recovery/$b
  mode=0644; [ -x "$H/$f" ] && mode=0755
  case $b in *.sh) mode=0755;; esac
  sed 's/\r$//' "$REPO/$f" > "$O/incoming.tmp"; install -m $mode "$O/incoming.tmp" "$H/$f"
  sha256sum "$H/$f" | sed "s#$H/##" >> $O/files.sha256
  if G cat-file -e "$C:$f" 2>/dev/null; then
    G show "$C:$f" | diff -u --label "a/$f" --label "b/$f" - "$H/$f" >> $O/harness.diff || true
  else
    diff -u --label /dev/null --label "b/$f" /dev/null "$H/$f" >> $O/harness.diff || true
  fi
done
: > "$O/incoming.tmp"
sha256sum $O/harness.diff; cat $O/files.sha256; grep -c '^[-+][^-+]' $O/harness.diff || true
( cd $H && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > $O/runcopy-files.sha256
diff $O/pristine-files.sha256 $O/runcopy-files.sha256 | grep '^[<>]' | sed 's/^\(.\) [0-9a-f]*  /\1 /' | sort | uniq -c | sed 's/^/differs: /' > $O/differs-from-archive.txt || true
cat $O/differs-from-archive.txt
echo "$(date -u +%FT%TZ) made run copy $name" >> $R/progress.txt
