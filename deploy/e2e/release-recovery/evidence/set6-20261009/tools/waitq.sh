#!/bin/bash
# set6 read-only: wait until no queue worker of this run is running (or LIMIT seconds passed), then print the state.
# usage: waitq.sh [LIMIT_SECONDS]
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
limit=${1:-3000}; t=0
while pgrep -f "set6/queue.sh" > /dev/null && [ $t -lt $limit ]; do sleep 10; t=$((t + 10)); done
bash $J/poll.sh
for f in /var/tmp/cp-set6-run/logs/stage-*.txt; do [ -f "$f" ] && { echo "--- $(basename $f)"; tail -n 14 "$f" | cut -c1-700; }; done 2>/dev/null | tail -n 60
