#!/bin/bash
# set4b: the Python harness suites on a scratch copy.  usage: py-suite.sh NAME REF|WORKTREE
#   REF: `git archive REF` of the working repository.  WORKTREE: `git archive HEAD` + the files of worktree-files.txt
#   (every file of the working tree that differs from HEAD under cmd, internal, web, docs and the harness folder).
export PYTHONDONTWRITEBYTECODE=1
R=/var/tmp/cp-set4b-run; name=$1; what=$2
REPO='/mnt/c/CELIKBROS PROJECTS/celikpanel'
T=$R/py-$name
[ -e $T ] && { echo "refusing: $T exists"; exit 2; }
mkdir -p $T
if [ "$what" = WORKTREE ]; then
  git -c safe.directory='*' -C "$REPO" archive HEAD | tar -x -C $T
  J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
  while IFS= read -r f; do
    f=$(echo "$f" | tr -d ''); [ -n "$f" ] || continue
    mkdir -p "$T/$(dirname "$f")"; sed 's/$//' "$REPO/$f" > "$T/$f"
  done < $J/worktree-files.txt
  echo "overlaid $(grep -c . $J/worktree-files.txt) files of the working tree"
else
  git -c safe.directory='*' -C "$REPO" archive "$what" | tar -x -C $T
fi
cd $T
python3 -m unittest discover -s deploy/e2e/release-recovery -p 'test_*.py' > $R/logs/py-$name.txt 2>&1
echo "rc=$? $(grep -E '^Ran ' $R/logs/py-$name.txt) $(tail -n 1 $R/logs/py-$name.txt)"
grep -E '^(FAIL|ERROR):' $R/logs/py-$name.txt | sort
