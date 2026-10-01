#!/bin/bash
cat /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*collect/journal-packagekit.txt | grep -v -E "^-- " | cut -c1-200 | tail -n ${2:-40}
