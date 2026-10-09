#!/bin/bash
# usage: stepfiles.sh LAB NODE STEP-GLOB [N] -> newest native files of a running step (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/$2/upd1/*/ | tail -1)
ls -lt --time-style=+%T $ev/steps/$3/native 2>/dev/null | head -n ${4:-12} | awk '{print $6, $7}'
date -u +%T
