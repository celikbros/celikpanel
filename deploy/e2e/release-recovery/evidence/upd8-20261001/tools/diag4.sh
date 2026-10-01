#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ls $L
grep -rl -i packagekit $L --include='*.json' --include='*.txt' --include='*.log' --include='*.jsonl' 2>/dev/null | head
f=$(ls $L/*baseline*log* 2>/dev/null | head -1); echo "log=$f"; ls -la $L | head -40
