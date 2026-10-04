SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
C=pdns-switch__target-started__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r2; S=z05-zero-started-zl-rb
L=/var/tmp/cp-b12-1001/logs/$S
python3 $SP/gssh.py $R $C arch "sudo journalctl --no-pager -o short-iso-precise --since '2026-09-30 08:53:00' --until '2026-09-30 08:54:10' | grep -v audit | grep -E 'celikpeer|bind-peer-inspect|named\[|Connection closed by 192.0.2.10|Accepted publickey for celikpeer' | grep -v -i -E 'secret'" < /dev/null > $L/secondary-inspector-window-previous-boot.txt 2>&1
grep -E 'Accepted publickey for celikpeer|COMMAND=/usr/local/libexec|zonestatus|127.0.0.1#|Connection closed by 192.0.2.10' $L/secondary-inspector-window-previous-boot.txt | cut -c1-220
bash $SP/collect12.sh $S $C $R > /dev/null 2>&1
grep -E 'rc=|request id|cell directory' $L/collect.log
bash $SP/held12.sh $S $C $R after-collect > /dev/null 2>&1
bash $SP/stop12.sh $S $C $R
echo Z05-FIN-DONE
