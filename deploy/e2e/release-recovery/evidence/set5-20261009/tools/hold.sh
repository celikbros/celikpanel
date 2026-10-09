#!/bin/bash
# set5: one idle WSL session held for the run (ends when /var/tmp/cp-set5-run/hold.stop exists or after 14 h)
for i in $(seq 1 1680); do [ -e /var/tmp/cp-set5-run/hold.stop ] && exit 0; sleep 30; done
