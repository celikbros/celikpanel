#!/bin/bash
cat /var/tmp/cp-set5-run/logs/stage-$1.txt | cut -c1-700
tail -n 5 /var/tmp/cp-set5-run/logs/queue.log
