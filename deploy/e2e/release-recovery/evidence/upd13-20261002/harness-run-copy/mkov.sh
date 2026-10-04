#!/bin/bash
mkdir -p /var/tmp/cp-upd13-run/overlay
: > /var/tmp/cp-upd13-run/overlay/files.sha256
echo "upd13: no overlay; the harness runs from git archive f6cdd5a0 unchanged" > /var/tmp/cp-upd13-run/overlay/README.txt
ls -la /var/tmp/cp-upd13-run/overlay
