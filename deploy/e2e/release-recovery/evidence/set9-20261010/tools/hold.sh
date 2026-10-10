#!/bin/bash
# set9: one idle WSL session held for the run (ends when /var/tmp/cp-set9-run/hold.stop exists or after 16 h)
mkdir -p /var/tmp/cp-set9-run
for i in $(seq 1 1920); do [ -e /var/tmp/cp-set9-run/hold.stop ] && exit 0; sleep 30; done
