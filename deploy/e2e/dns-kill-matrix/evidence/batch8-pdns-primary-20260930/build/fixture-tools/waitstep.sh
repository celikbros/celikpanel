# usage: waitstep.sh SHORT PATTERN MAXSEC [SKIPLINES] -- returns when driver.log (after SKIPLINES lines) matches PATTERN
# (or ALLDONE/NO-COLLECT/FAILED) or after MAXSEC
L=/var/tmp/cp-b8-0930/logs/$1/driver.log
K=$(( ${4:-0} + 1 ))
end=$(( $(date +%s) + $3 ))
while [ "$(date +%s)" -lt "$end" ]; do
  tail -n +$K "$L" 2>/dev/null | grep -qE "$2|ALLDONE|NO-COLLECT|FAILED" && break
  sleep 10
done
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8/poll.sh $1 25 10
