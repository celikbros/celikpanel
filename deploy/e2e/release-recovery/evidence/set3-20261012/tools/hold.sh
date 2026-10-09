#!/bin/bash
# set3: one idle WSL session held for the run (ends when /var/tmp/cp-set3-run/hold.stop exists or after 10 h)
for i in $(seq 1 1200); do [ -e /var/tmp/cp-set3-run/hold.stop ] && exit 0; sleep 30; done
