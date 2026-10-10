#!/bin/bash
awk '{print $1, $2, $3, $4}' /var/tmp/cp-set5-run/mem-watch.txt | awk '{split($3,a,"="); if (a[2]!=p) print; p=a[2]}' | tail -12
sort -t= -k2 -n /var/tmp/cp-set5-run/mem-watch.txt | head -0
awk '{split($2,a,"="); if (min=="" || a[2]<min) {min=a[2]; l=$0}} END {print "lowest:", l}' /var/tmp/cp-set5-run/mem-watch.txt
grep -E 'T18:1[0-4]' /var/tmp/cp-set5-run/mem-watch.txt | cut -c1-200
