#!/bin/bash
# read-only: the start check's command line (listen value) in one lab's product journal
f=$(ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*collect/journal-product.txt | tail -1)
grep -n 'check-startup-readiness' "$f" | grep -o 'CELIKPANEL_LISTEN=[^ ]*\|^[0-9]*:[^ ]*' | paste - - | head -5
grep -n -o 'Environment=CELIKPANEL_LISTEN=[^ ]*' "$f" | head -3
