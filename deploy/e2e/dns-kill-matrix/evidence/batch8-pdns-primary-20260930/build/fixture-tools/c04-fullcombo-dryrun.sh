# Dry run only (no --execute): records the admission answer for the full requested flag combination of cell 4.
cd /root/cp-b8-src
L=/var/tmp/cp-b8-0930/logs/c04-pri-started-zl
python3 deploy/e2e/dns-kill-matrix/guest_bootstrap.py run-prepared --work-root /var/tmp/cp-b8-0930 --cell-id pdns-switch__target-started__after-write__paired-primary__peer-reachable --node debian13 --identity-file /var/tmp/cp-b8-0930/id_ed25519 --source-fixture uninitialized --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot > $L/run-prepared-dryrun-full-combination.log 2>&1 < /dev/null
echo "rc=$?" >> $L/run-prepared-dryrun-full-combination.log
cat $L/run-prepared-dryrun-full-combination.log
