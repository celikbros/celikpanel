#!/bin/bash
L=/var/tmp/cp-release-drill-$1
ev=$(ls -d $L/evidence/*/upd1/*/ | tail -1)
cd $ev/steps
grep -h -i -E 'busy|unattended|apt-daily|dpkg|lock|package-manager|mutation' */journal-*.txt 2>/dev/null | cut -c1-260 | head -40
echo ----lab
head -c 3000 */journal-lab.txt
echo ----sizes; wc -l */journal-*.txt
