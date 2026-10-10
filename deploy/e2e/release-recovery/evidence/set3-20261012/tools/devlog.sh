#!/bin/bash
# usage: devlog.sh TEST  -> the failures of the last dev run of that suite, bounded
grep -n -A14 '^FAIL:\|^ERROR:' /var/tmp/cp-set3-run/logs/dev-$1.txt | cut -c1-260 | head -n ${2:-70}
