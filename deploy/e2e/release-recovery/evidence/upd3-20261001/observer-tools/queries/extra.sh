#!/bin/bash
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd3-20261001'
find . -type f ! -name SHA256SUMS | sort > /tmp/cp-upd3-list-a.txt
sed 's/^[0-9a-f]*  //' SHA256SUMS | sort > /tmp/cp-upd3-list-b.txt
comm -3 /tmp/cp-upd3-list-a.txt /tmp/cp-upd3-list-b.txt
rm -f /tmp/cp-upd3-list-a.txt /tmp/cp-upd3-list-b.txt
