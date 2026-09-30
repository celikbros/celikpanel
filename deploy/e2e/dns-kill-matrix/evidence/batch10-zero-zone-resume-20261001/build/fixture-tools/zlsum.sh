SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
for c in z04-resume z05-held-resume; do echo "######## $c"; python3 $SP/zlsum.py /var/tmp/cp-b10-1001/evidence/$c; done
