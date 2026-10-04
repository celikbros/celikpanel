#!/bin/bash
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
c=$ev/steps/14-collect
grep -n -i 'package\|apt\|dpkg\|unattended\|busy\|05-mail_profile\|mail_profile' $c/journal-product.txt | grep -i -v 'milter chain: nothing' | sed -n "${2:-1},${3:-60}p" | cut -c1-330
echo ---- setup-services
grep -n -i 'apt-daily\|unattended\|dpkg\|apt\b\|apt-get\|man-db' $c/journal-setup-services.txt $c/journal-lab.txt | cut -c1-250 | head -40
