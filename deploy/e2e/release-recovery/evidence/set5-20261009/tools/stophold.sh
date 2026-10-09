#!/bin/bash
# set5: end the idle WSL session and the memory watcher of this run
date -u +%FT%TZ > /var/tmp/cp-set5-run/hold.stop
echo "$(date -u +%FT%TZ) hold.stop written (the idle session and the memory watcher end within 30 s)" >> /var/tmp/cp-set5-run/progress.txt
