#!/bin/bash
L=/var/tmp/cp-release-drill-$1
du -sh $L; find $L -size +5M -printf '%s %n %p\n' | sort -n | tail -20; du -sh $L/cells/* $L/images $L/evidence
