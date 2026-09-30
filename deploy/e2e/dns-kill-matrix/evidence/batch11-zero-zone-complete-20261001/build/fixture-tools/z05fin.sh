SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b11
C=pdns-switch__target-started__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b11-1001/r2; S=z05-zero-started-zl-rb
bash $SP/z05observe.sh
bash $SP/collect11.sh $S $C $R
bash $SP/held11.sh $S $C $R after-collect
echo Z05-FIN-DONE
