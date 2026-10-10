#!/bin/sh
# set4c: the Go verification on the tree as it stands.  usage: go-final.sh NAME   -> logs go-NAME-*.txt/json beside this script
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel' || exit 2
export GOTOOLCHAIN=local
GO=/opt/celikpanel-s2/go/bin/go
OUT=$(dirname "$0"); n=$1
{ date -u +%FT%TZ; $GO version; } > "$OUT/go-$n-id.txt"
/opt/celikpanel-s2/go/bin/gofmt -l cmd internal > "$OUT/go-$n-gofmt.txt" 2>&1; echo "gofmt rc=$? files=$(wc -l < "$OUT/go-$n-gofmt.txt")" >> "$OUT/go-$n-id.txt"
$GO vet ./cmd/agent/... ./cmd/panel/... ./internal/... > "$OUT/go-$n-vet.txt" 2>&1; echo "vet rc=$?" >> "$OUT/go-$n-id.txt"
GOOS=linux GOARCH=amd64 $GO build ./cmd/... ./internal/... > "$OUT/go-$n-build-amd64.txt" 2>&1; echo "build amd64 rc=$?" >> "$OUT/go-$n-id.txt"
GOOS=linux GOARCH=arm64 $GO build ./cmd/... ./internal/... > "$OUT/go-$n-build-arm64.txt" 2>&1; echo "build arm64 rc=$?" >> "$OUT/go-$n-id.txt"
$GO test ./cmd/agent/... -count=1 -json > "$OUT/go-$n-agent.json" 2> "$OUT/go-$n-agent.err"; echo "test agent rc=$?" >> "$OUT/go-$n-id.txt"
$GO test ./cmd/panel/... ./internal/... -count=1 -json > "$OUT/go-$n-other.json" 2> "$OUT/go-$n-other.err"; echo "test panel+internal rc=$?" >> "$OUT/go-$n-id.txt"
date -u +%FT%TZ >> "$OUT/go-$n-id.txt"
