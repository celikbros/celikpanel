#!/bin/sh
# set4b: the Panel and Agent tests that read docs/ and web/src (not the final run).
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
GO=/opt/celikpanel-s2/go/bin/go
/opt/celikpanel-s2/go/bin/gofmt -l cmd internal 2>&1 | head
$GO test ./cmd/panel/ -count=1 2>&1 | tail -12
