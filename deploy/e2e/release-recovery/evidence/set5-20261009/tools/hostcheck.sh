#!/usr/bin/env bash
# set5: read-only host check before the run.
date -u +%FT%TZ
echo "--- whoami"; id -un
echo "--- uptime"; uptime
echo "--- memory"; free -m
echo "--- cpus"; nproc
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set5*"; ls -d /var/tmp/cp-set5* /var/tmp/cp-release-drill-s5* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|request_identity|runall|lab.py|set4_|set4b_|go test|go build' | grep -v pgrep | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo '--- 67b62cc0f'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse '67b62cc0f^{commit}' '67b62cc0f^{tree}'
echo '--- tag alpha.81'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'v0.1.0-alpha.81^{commit}' 'v0.1.0-alpha.81^{tree}'
echo "--- builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null | tail -n 12
echo "--- dist dirs"; ls /var/tmp/cp-pair-accept/dist/ 2>/dev/null | wc -l; ls -d /var/tmp/cp-pair-accept/dist/a0beb7263* /var/tmp/cp-pair-accept/dist/67b62cc0f* 2>/dev/null || echo "no dist of the tag commit"
echo "--- a81 dist"; ls -la /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/ 2>/dev/null; sha256sum /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/*.tar* 2>/dev/null
echo "--- git local config key list"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
echo "--- var/tmp count"; ls /var/tmp | wc -l
echo "--- var/tmp disk"; df -h /var/tmp | tail -1; df -B1 /var/tmp | tail -1
echo "--- listening ports 4xxx / 18xxx"; ss -ltn 2>/dev/null | grep -E ':(4[0-9]{3}|18[0-9]{3})\b' || echo none
echo "--- labs"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l
echo "--- sizes of set4 labs (what a lab leaves without overlay)"; du -sh /var/tmp/cp-release-drill-s4-u9-d13-b /var/tmp/cp-release-drill-s4-u9-ub-a /var/tmp/cp-release-drill-s4-u12-arch-b 2>/dev/null; du -sh /var/tmp/cp-release-drill-s4-u9-d13-b/* 2>/dev/null
echo "--- set4 run dir"; du -sh /var/tmp/cp-set4-run /var/tmp/cp-set4b-run 2>/dev/null; ls /var/tmp/cp-set4-run | head -40
echo "--- build dir sizes"; du -sh /var/tmp/cp-upd1-build/20261009t* 2>/dev/null
echo "--- fstrim/discard"; mount | grep ' / ' ; cat /proc/mounts | grep -E ' / |/var' | head
