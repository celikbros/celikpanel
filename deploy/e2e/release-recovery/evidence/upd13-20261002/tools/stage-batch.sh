#!/bin/bash
# usage: stage-batch.sh SPEC... (SHORT:LAB:NODE:CELLDIR:RUN); stages each, then removes its overlay disks only when
# staged OK and the run is complete-for-review (upd13: a run that is not complete keeps its overlays for diagnosis;
# they are removed later with rmoverlay13.sh once diagnosed).
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
for spec in "$@"; do
  IFS=: read -r short lab node celldir run <<< "$spec"
  out=$(bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13/stagecell13.sh "$spec"); echo "$out"
  overall=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('overall'))" "$S/$celldir/$run/result.json" 2>/dev/null)
  if ! echo "$out" | grep -q '^STAGED-OK '; then echo "NOT REMOVED: $lab (staging did not verify)"
  elif [ "$overall" != complete-for-review ]; then echo "KEPT overlays of $lab for diagnosis (overall=$overall)"
  else bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13/rmoverlay13.sh $lab $celldir $run; fi
done
