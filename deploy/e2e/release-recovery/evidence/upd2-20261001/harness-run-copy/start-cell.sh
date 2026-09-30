#!/bin/bash
D=$(dirname "$0")
pgrep -a qemu && { echo "QEMU already running; refusing"; exit 2; }
free -g | head -2
bash $D/bg.sh cell-$1 $D/job-cell-$1.sh
bash $D/bg.sh side-$1 $D/job-side-$1.sh
