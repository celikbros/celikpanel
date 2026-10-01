#!/usr/bin/env bash
# Read-only host check before upd8.
date -u
echo "--- memory"; free -m
echo "--- disk (WSL-internal)"; df -BG /var/tmp / 2>/dev/null
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- any ubuntu image on host"; find / -xdev \( -iname '*ubuntu*.img' -o -iname '*noble*' -o -iname '*ubuntu*.qcow2' \) -size +100M 2>/dev/null | grep -v '^/mnt/' | head
echo "--- existing cp-upd8*"; ls -d /var/tmp/cp-upd8* /var/tmp/cp-release-drill-upd8* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|runall' | cut -c1-160 || echo none
echo "--- tools"; command -v genisoimage xorriso qemu-img curl sha256sum gpg
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo "--- alpha80"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'v0.1.0-alpha.80^{commit}'
