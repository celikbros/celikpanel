#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
cd $ev/steps
grep -h -E 'panel\[|agent\[' */journal-product.txt | grep -v 'TLS handshake' | grep -E '13:4[1-3]' | cut -c1-600 | tail -25
