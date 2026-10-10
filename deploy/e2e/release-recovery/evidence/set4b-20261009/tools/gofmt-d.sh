#!/bin/sh
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
/opt/celikpanel-s2/go/bin/gofmt -d cmd/panel/set4_corrections_test.go | head -30
