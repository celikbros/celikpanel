#!/bin/bash
S=/var/tmp/cp-upd4-run/stage/upd4-20261001
cd $S
echo "185.95.:"; grep -rlF '185.95.' . | head
echo "inspect-after-continuation-20260930T19:"; grep -rlF 'inspect-after-continuation-20260930T19' . | head
echo "ssh key prefix:"; grep -rlF 'AAAAC3NzaC1lZDI1NTE5' . | head
grep -rhoF 'inspect-after-continuation-20260930T193606Z' . | head -2
grep -rh 'inspect-after-continuation-20260930T193606Z' . | head -2 | cut -c1-300
