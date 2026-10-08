#!/bin/bash
uptime; echo "journald since: $(systemctl show systemd-journald -p ActiveEnterTimestamp --value)"
pgrep -af 'qemu-system|settings_writes_trial|run-set1' | cut -c1-140
echo "--- out"; tail -n 5 /var/tmp/cp-set1-run/logs/cell-${1:-d13}.out | cut -c1-500
echo "--- err"; tail -n 5 /var/tmp/cp-set1-run/logs/cell-${1:-d13}.err | cut -c1-500
