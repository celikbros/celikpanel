#!/bin/bash
grep -h -i -E "packagekit|package manager|idle" /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*collect/journal-product.txt | grep -E "T20:1[6-9]" | cut -c1-260 | tail -n 25
