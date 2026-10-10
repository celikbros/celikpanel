#!/bin/bash
# set6: end the held WSL session of the run, then finalize the evidence folder. usage: end.sh COPY
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
touch /var/tmp/cp-set6-run/hold.stop
for i in $(seq 1 20); do pgrep -f "set6/hold.sh" > /dev/null || break; sleep 2; done
echo "$(date -u +%FT%TZ) the held session ended; finalize" >> /var/tmp/cp-set6-run/progress.txt
bash $J/finalize.sh $1
