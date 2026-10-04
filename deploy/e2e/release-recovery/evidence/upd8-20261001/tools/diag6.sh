#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
cd $ev/steps
grep -h -i -E 'mail|postfix|dovecot|vmail|failed|error' */journal-*.txt 2>/dev/null | grep -v 'TLS handshake' | cut -c1-500 | tail -30
