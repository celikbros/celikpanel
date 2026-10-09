#!/bin/bash
# set2: the Arch request-identity cell is stopped before its Let's Encrypt section, because the lab's ACME isolation was
# not confirmed on that guest (C0). Only the driver is signalled; the wrapper then stops the lab as usual.
pid=$(pgrep -f "request_identity_trial.py run --cell rid-arch" | head -n 1)
echo "driver pid=$pid at $(date -u +%FT%TZ)"
grep '"step"' /var/tmp/cp-set2-run/logs/cell-rid-arch.out | tail -n 3 | cut -c1-200
[ -n "$pid" ] && kill -TERM $pid
sleep 3
pgrep -af "request_identity_trial.py run --cell rid-arch" | cut -c1-80 || echo "driver gone"
