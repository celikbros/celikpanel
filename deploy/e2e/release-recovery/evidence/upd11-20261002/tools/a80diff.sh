#!/bin/bash
# upd11: the alpha.80 build's fixture patches as upd7 records them (replaces the over-wide first version).
set -u
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd11-20261002"
CL=/var/tmp/cp-upd1-build/20261001t221533z/repo
G="git -c safe.directory=* -C $CL"
TAG=bd14d97efc5cfd19acd70ddf0edb9c6343317e2b
SRC=48e5465712c3ef94f10fa8f91cc952289902ec2d
ls -la "$S/build/a80/fixture-patches.diff"
{ echo "=== tag v0.1.0-alpha.80 ($TAG) .. baseline (D-027 licence seam only)"; $G diff $TAG refs/upd1/baseline
  echo "=== source $SRC .. good (release policy label only)"; $G diff $SRC refs/upd1/good
  echo "=== good .. defective"; $G diff refs/upd1/good refs/upd1/defective; } > "$S/build/a80/fixture-patches.diff"
$G diff --stat $TAG refs/upd1/baseline | tail -1; $G diff --stat $SRC refs/upd1/good | tail -1
wc -l "$S/build/a80/fixture-patches.diff"; wc -l "$S/build/cur/fixture-patches.diff"
du -sh "$S"
