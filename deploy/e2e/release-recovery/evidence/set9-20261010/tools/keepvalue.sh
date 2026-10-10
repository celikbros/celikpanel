#!/bin/bash
# set9: a root-only copy of one window's password file, kept ONLY so that the secret scan can search the evidence for
# the value; removed after the scan (scanvalues/ is never staged). usage: keepvalue.sh HAND
set -eu
install -d -m 0700 /var/tmp/cp-set9-run/scanvalues
install -m 0600 /var/tmp/cp-set9-run/secret/$1.json /var/tmp/cp-set9-run/scanvalues/$1.json
ls -la /var/tmp/cp-set9-run/scanvalues | tail -n +2 | awk '{print $1, $NF}'
