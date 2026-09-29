# usage: waitfor.sh SHORT MAXSEC  -- returns when the cell's driver.log has ALLDONE/NO-COLLECT or after MAXSEC
L=/var/tmp/cp-b8-0930/logs/$1/driver.log
end=$(( $(date +%s) + $2 ))
while [ "$(date +%s)" -lt "$end" ]; do
  grep -qE 'ALLDONE|NO-COLLECT' "$L" 2>/dev/null && break
  sleep 10
done
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8/poll.sh $1 12 6
