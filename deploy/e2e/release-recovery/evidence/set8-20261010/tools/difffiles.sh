#!/bin/bash
# set8: compare kept vhost texts of one lab (read only). usage: difffiles.sh LAB NODE FILE_A FILE_B   (paths relative to the run dir)
d=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/set8-*/ | tail -1)
cd "$d" && sha256sum "$3" "$4" && diff -u "$3" "$4"
grep -h '"group"\|"owner"\|"mode"\|"inode"' steps/09-s0-sites/native/*s0-baseline-read.json | head -8
