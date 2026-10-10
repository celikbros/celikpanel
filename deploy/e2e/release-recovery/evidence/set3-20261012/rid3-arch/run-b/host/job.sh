#!/bin/bash
# set3: the second Arch reading: the same cell with ONE recorded owner action (the nginx snippet the PHP vhost includes).
export PYTHONDONTWRITEBYTECODE=1
export SET3_OWNER_NGINX_PHP_SNIPPET=1
exec bash /var/tmp/cp-set3-run/harness-e/deploy/e2e/release-recovery/run-set2.sh cell rid3-arch /var/tmp/cp-upd1-build/20261009t061555z/upd1-artifacts.json rid3-arch-b 4411 18481
