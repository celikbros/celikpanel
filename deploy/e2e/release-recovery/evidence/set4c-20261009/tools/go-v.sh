#!/bin/sh
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
/opt/celikpanel-s2/go/bin/go test ./cmd/agent/ -count=1 -v -run 'TestPostconfValue|TestMailTLSSnapshotKeeps|TestMailTLSRefusesAtTheSnapshot|TestMailTLSRestoreNever|TestMailTLSReadBackCompares|TestPostconfExpandedRead' 2>&1 | grep -E '^(=== RUN|--- |ok|FAIL|PASS|\s+set4c)' | grep -v '=== RUN'
