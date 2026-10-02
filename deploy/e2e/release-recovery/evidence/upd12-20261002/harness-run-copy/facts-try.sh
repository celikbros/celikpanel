#!/bin/bash
# upd12: print the generated read-only file-facts script and run it on the WSL host (files there are absent; read-only).
export PYTHONDONTWRITEBYTECODE=1
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery'
python3 -c "import sys; sys.path.insert(0, '.'); import owner_update_trial as m; open('/tmp/upd12-facts.sh','w').write(m.mail_file_facts_script())"
cat /tmp/upd12-facts.sh
echo ---
printf 'x\n' > /tmp/upd12-probe.txt
sed 's#/etc/aliases.db#/tmp/upd12-probe.txt#' /tmp/upd12-facts.sh | bash -eu
rm -f /tmp/upd12-facts.sh /tmp/upd12-probe.txt
