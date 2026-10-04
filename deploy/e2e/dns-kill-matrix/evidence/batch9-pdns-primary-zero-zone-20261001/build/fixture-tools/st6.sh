awk '/######## z06/{p=1} p' /var/tmp/cp-b9-1001/status-texts-all-cells.txt | cut -c1-2500 | head -40
echo ==== z06 owner edit
python3 /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9/findk.py z06-zero-owner-sql pdns-switch__target-started__after-write__paired-primary__peer-reachable edit 1500 | head -5
