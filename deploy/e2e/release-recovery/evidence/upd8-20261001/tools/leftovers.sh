#!/bin/bash
# read-only: what this run created on the host, with sizes
date -u +%FT%TZ
du -sh /var/tmp/cp-upd8-run /var/tmp/cp-upd8-img /var/tmp/cp-upd8-quick /var/tmp/cp-release-drill-upd8-* /var/tmp/cp-upd1-build/20261001t125717z 2>/dev/null
for c in 57f4372c0485cc2e568b45d5b9c05785d3633320 8c71415e56a2b9a13232377d501cf4bb8a23a9c8 80d7a43fdd326d7155da9d671836ba1e054b07b3; do du -sh /var/tmp/cp-pair-accept/dist/$c-acceptance-license 2>/dev/null; done
stat -c '%n links=%h size=%s' /var/tmp/cp-v3n28/images/ubuntu-24.04-server-cloudimg-amd64-20260826.img /var/tmp/cp-install-vm/images/ubuntu-24.04-20260826.img
pgrep -a qemu | cut -c1-100 || echo "no qemu process"
df -BG /var/tmp | tail -1
git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
