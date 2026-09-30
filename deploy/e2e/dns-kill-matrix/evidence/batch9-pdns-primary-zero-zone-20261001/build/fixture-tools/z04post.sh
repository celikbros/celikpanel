SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b9
R=/var/tmp/cp-b9-1001/r2; C=pdns-switch__committed__after-write__paired-primary__peer-reachable
E=/var/tmp/cp-b9-1001/evidence/z04-zero-committed-zl/rec
{
echo "### $(date -u +%FT%T.%3NZ) read-only: owner tool hashes on the secondary (the enroll.log glob ran unprivileged and failed; installs had succeeded)"
python3 $SP/gssh.py $R $C arch "sudo sh -c 'ls -la /root/dns-owner-tools; sha256sum /root/dns-owner-tools/*'" < /dev/null
echo "### $(date -u +%FT%T.%3NZ) read-only: ledger jobs and agent journal on the primary after the refused recover"
python3 $SP/gssh.py $R $C debian13 'sudo bash -s' < $SP/ledq_remote.sh | grep -v 'Panel certificate activation remains pending'
echo "### $(date -u +%FT%T.%3NZ) read-only: pending deletion job, full ledger entry"
python3 $SP/gssh.py $R $C debian13 "sudo python3 -c 'import json;d=json.load(open(\"/var/lib/celikpanel-agent-private/service-mutations.json\"));[print(json.dumps(j,indent=1,sort_keys=True)) for j in d[\"jobs\"] if j.get(\"request_id\")==\"7fb6a0df9a0df4c78fa75b362d0a2988\"]'" < /dev/null
} > $E/pending-delete-after-refused-recover.txt 2>&1
wc -l $E/pending-delete-after-refused-recover.txt
grep -v '^ *"' $E/pending-delete-after-refused-recover.txt | head -30
python3 -c 'import json;d=[l for l in open("'$E'/pending-delete-after-refused-recover.txt")];print("".join(d[-60:]))'
