#!/bin/sh
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
OUT=$(dirname "$0")
/opt/celikpanel-s2/go/bin/go test ./cmd/agent/... -count=1 -json > "$OUT/go-interim-agent.json" 2> "$OUT/go-interim-agent.err"
echo "rc=$?" > "$OUT/go-interim-agent.rc"
