#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-rid-arch-b/evidence/arch/upd1/rid-arch-*/ | tail -1)
ls $ev/steps/*collect*/ | tr '\n' ' '; echo
grep -h "T04:33:[2-5]" $ev/steps/*collect*/journal-product.txt | grep -v "GET \|audit" | cut -c1-420 | head -30
