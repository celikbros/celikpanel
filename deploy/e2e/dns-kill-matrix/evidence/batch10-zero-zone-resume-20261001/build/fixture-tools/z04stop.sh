SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/b10
echo "### $(date -u +%FT%T.%3NZ) stop z04 guests (QMP quit, overlays kept; resumed delete still pending dns_peer_inspection_unknown)" >> /var/tmp/cp-b10-1001/logs/z04-resume/driver.log
bash $SP/stoponly.sh z04-resume pdns-switch__committed__after-write__paired-primary__peer-reachable /var/tmp/cp-b9-1001/r2
pgrep -af qemu-system || echo "no qemu process"
