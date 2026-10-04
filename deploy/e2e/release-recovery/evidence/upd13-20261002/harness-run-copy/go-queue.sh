#!/bin/bash
# usage: go-queue.sh NAME LISTFILE(basename in the scratch dir)
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
cat > /var/tmp/cp-upd13-run/jobs/job-queue-$1.sh <<EOT
#!/bin/bash
exec bash $P/queue.sh $P/$2
EOT
cp $P/$2 /var/tmp/cp-upd13-run/jobs/queue-$1.list
bash $P/bg.sh queue-$1 /var/tmp/cp-upd13-run/jobs/job-queue-$1.sh
