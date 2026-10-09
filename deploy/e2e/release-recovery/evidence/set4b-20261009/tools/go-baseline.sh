#!/bin/sh
# set4b: cmd/agent on the tree before any product edit (HEAD e2be8af30), for the failing-set comparison.
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
OUT=<scratchpad>/set4b
{ date -u +%FT%TZ; id; git -c safe.directory='*' rev-parse HEAD; git -c safe.directory='*' status --short -- cmd internal web docs; } > "$OUT/go-baseline-id.txt"
/opt/celikpanel-s2/go/bin/go test ./cmd/agent/... -count=1 -json > "$OUT/go-baseline-agent.json" 2> "$OUT/go-baseline-agent.err"
echo "rc=$?" >> "$OUT/go-baseline-id.txt"
date -u +%FT%TZ >> "$OUT/go-baseline-id.txt"
