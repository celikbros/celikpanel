#!/bin/bash
pgrep -af 'python3 - /var/tmp/cp-release-drill-upd8-probe-a' | cut -c1-120
pkill -f 'python3 - /var/tmp/cp-release-drill-upd8-probe-a' && echo killed
