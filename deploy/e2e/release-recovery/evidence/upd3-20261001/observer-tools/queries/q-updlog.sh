echo "== files changed 17:54:00-17:54:40 (excluding /proc /sys /run /dev)"
find / -xdev \( -path /proc -o -path /sys -o -path /dev \) -prune -o -newermt '2026-09-30 17:54:00' ! -newermt '2026-09-30 17:54:40' -type f -print 2>/dev/null | grep -v -E '^/var/lib/celikpanel/.*\.db-(wal|shm)$' | head -80
echo "== /var/log listing"; ls -la /var/log | head -40; ls -la /var/log/celikpanel* 2>/dev/null
echo "== recovery-runtimes"; ls -laR /usr/libexec/celikpanel/recovery-runtimes 2>/dev/null | head -40
echo "== recovery runtime-status"; /usr/libexec/celikpanel/recovery runtime-status --lang en 2>&1 | head -40
