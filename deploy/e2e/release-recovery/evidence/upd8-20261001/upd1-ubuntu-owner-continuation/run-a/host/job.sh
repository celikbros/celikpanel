#!/bin/bash
export PYTHONDONTWRITEBYTECODE=1
exec bash /var/tmp/cp-upd8-run/harness/deploy/e2e/release-recovery/run-upd1.sh cell upd1-ubuntu-owner-continuation /var/tmp/cp-upd1-build/20261001t125717z/upd1-artifacts.json upd8-ub-oc-a 2851
