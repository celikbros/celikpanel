# Cell 2 (z05 fresh): set up work root r2, then cell.sh (prepare .. run-prepared) in the background; no collect (a held run must not be disturbed)
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/p7b12/b12
if pgrep -f qemu-system-x86_64 >/dev/null; then echo "QEMU running: not started"; exit 1; fi
bash $SP/setup.sh /var/tmp/cp-b12-1001/r2; echo "setup rc=$?"
grep -E 'SETUP-OK|rror' /var/tmp/cp-b12-setup.log | tail -3
echo "$(date -u +%FT%TZ) start z05-zero-started-zl-rb (cell.sh; no collect until after the resume)" >> /var/tmp/cp-b12-1001/logs/chain.log
setsid -f nohup bash $SP/cell.sh z05-zero-started-zl-rb pdns-switch__target-started__after-write__paired-primary__peer-reachable /var/tmp/cp-b12-1001/r2 0 --zero-zones --zone-lifecycle --reboot-after-recovery --disable-management-before-reboot > /dev/null 2>&1 < /dev/null
echo launched
