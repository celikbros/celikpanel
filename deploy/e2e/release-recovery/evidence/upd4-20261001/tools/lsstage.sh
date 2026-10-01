#!/bin/bash
# usage: lsstage.sh CELL [RUN] -> list staged files except samples/inspections
S=/var/tmp/cp-upd4-run/stage/upd4-20261001
cd $S/$1/${2:-run-a} && find . -type f | grep -v '/samples/' | grep -v 'inspect-before-site' | sort | head -200
