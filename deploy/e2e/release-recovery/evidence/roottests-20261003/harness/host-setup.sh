#!/bin/bash
# Host side (archlinux WSL): run copy of the lab code, toolchain tarball, guest prepare+start.
set -euo pipefail
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/roottests
W=/var/tmp/cp-roottests-20261003
export PYTHONDONTWRITEBYTECODE=1
date -u +%FT%TZ
if [ -e "$W" ]; then echo "work dir exists; refusing"; exit 1; fi
mkdir -m 0700 "$W" "$W/src"
cp -- "$SP/src-48d264d5.tar" "$W/src-48d264d5.tar"
sha256sum "$W/src-48d264d5.tar"
tar -C "$W/src" -xf "$W/src-48d264d5.tar" deploy/e2e/release-recovery deploy/e2e/dns-kill-matrix
tar -C /opt/celikpanel-test-toolchains/go1.26.5 -czf "$W/go1.26.5.tar.gz" go
sha256sum "$W/go1.26.5.tar.gz"
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
cp -- "$SP/rtlab.py" "$W/rtlab.py"
cd "$W"
python3 rtlab.py prepare
python3 rtlab.py start
date -u +%FT%TZ
echo HOST-SETUP-DONE
