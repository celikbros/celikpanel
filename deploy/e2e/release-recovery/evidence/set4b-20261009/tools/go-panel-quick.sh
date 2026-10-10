#!/bin/sh
# set4b: quick check of the Panel package after the import change (not the final run).
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
GO=/opt/celikpanel-s2/go/bin/go
$GO build ./cmd/panel/ 2>&1 | head -20
$GO vet ./cmd/panel/ 2>&1 | head -20
$GO test ./cmd/panel/ -count=1 -run 'Import|Cpmove' 2>&1 | tail -15
