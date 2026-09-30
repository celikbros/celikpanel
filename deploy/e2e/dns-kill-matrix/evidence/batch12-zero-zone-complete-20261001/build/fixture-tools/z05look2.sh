SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
L=/var/tmp/cp-b12-1001/logs/z05-zero-started-zl-rb
C=pdns-switch__target-started__after-write__paired-primary__peer-reachable; R=/var/tmp/cp-b12-1001/r2
bash $SP/post12.sh z05-zero-started-zl-rb $C $R "2026-09-30 08:53:00" "2026-09-30 08:54:00" after-resume > /dev/null 2>&1
grep -E 'step times|inspector answer|stopped at step|admitted the PowerDNS|observed different|remains pending|dns_peer_|Recover|recover' $L/agent-journal-all-boots.txt | cut -c1-600
echo "== secondary celikpeer lines"; grep -E 'Accepted publickey for celikpeer|celikpanel-bind-peer-inspect|zonestatus|Connection closed by 192.0.2.10' $L/secondary-after-resume.txt | cut -c1-200
echo "== pdns CATALOG-HASH (all boots)"
python3 $SP/gssh.py $R $C debian13 "sudo journalctl -u pdns.service --no-pager -o short-iso-precise | grep 'CATALOG-HASH' | cut -c1-200" < /dev/null
echo "== after-reboot verdict"
CD=$(python3 -c 'import sys;sys.path.insert(0,"/root/cp-b12-src/deploy/e2e/dns-kill-matrix");import fixture;from pathlib import Path;print(fixture.load_cell_plan(Path(sys.argv[1]).resolve(),sys.argv[2])["cell_directory"])' $R $C)
python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(json.dumps({k:d.get(k) for k in d if k!="observation"})[:1500]); o=d.get("observation") or {}; print({k:o.get(k) for k in ("catalog_members","catalog_members_expected","catalog_serial","serial_rule","rndc_status_ok","authoritative_udp_tcp")})' $CD/fresh-primary-peer/peer-verdict-after-reboot.json
grep -o '"serial_rule[^,]*' $CD/fresh-primary-peer/peer-verdict-after-reboot.json | head; grep -o 'post-publication[^"]*' $CD/fresh-primary-peer/peer-verdict-after-reboot.json | head -3
