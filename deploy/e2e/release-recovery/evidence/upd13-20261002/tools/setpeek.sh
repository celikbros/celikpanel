#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
ls $ev/steps/06-setup/ | tr '\n' ' '; echo
tail -n 3 /var/tmp/cp-upd13-run/logs/cell-$2.err | cut -c1-300
