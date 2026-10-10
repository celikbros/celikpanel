#!/bin/bash
# set9: stage the three labs, then what is common.
echo set9-20261010 > /var/tmp/cp-set9-run/evidence-name.txt
J=<scratchpad>/set9/tools
bash $J/stage-lab.sh cell s9-d13 lab1-cells-1-2-4-and-update-1 <scratchpad>/set9/blab1 2>&1 | tail -n 12
bash $J/stage-lab.sh cellb s9-d13b lab2-update-2 <scratchpad>/set9/blab2 2>&1 | tail -n 12
bash $J/stage-lab.sh cellc s9-d13c lab3-update-3-timed-click <scratchpad>/set9/blab3 2>&1 | tail -n 12
bash $J/stage-common.sh 2>&1 | tail -n 5
