#!/bin/bash
pgrep -af 'git -c safe.directory' | grep -v pgrep | cut -c1-200 || true
pgrep -af wtcheck.sh | grep -v pgrep | cut -c1-120 || true
ls -la /var/tmp/cp-set5-run/working-tree-against-copy-e.txt 2>/dev/null; tail -n 6 /var/tmp/cp-set5-run/working-tree-against-copy-e.txt 2>/dev/null | cut -c1-200
