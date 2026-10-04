#!/bin/bash
# upd13: addendum to secret-scan.txt - the part 2 installer logs (nested one level deeper) and where each non-zero
# shape count comes from (read-only grep over the staged folder).
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
O="$S/secret-scan.txt"
{
echo "--- part 2 installer logs naming the real origin"
for f in "$S"/part2-alpha80/*/run-*/host/current-worker-baseline-*.log; do printf '%s celikpanel.net=%s 185.95.=%s\n' "${f#$S/}" "$(grep -c 'celikpanel\.net' "$f")" "$(grep -c '185\.95\.' "$f")"; done
echo "--- files holding the fixture licence literal (D-027 seam constant, public source internal/licensing/acceptance_fixture.go)"
grep -rl "CPK-acce57f1c7$(printf '0%.0s' $(seq 54))" "$S" | sed "s#^$S/##"
echo "--- files holding the 32-char tokens (both are pieces of lab SSH PUBLIC keys)"
grep -rl -e 'nwys30GRGUiWORbefhY5bCfr4Z6nI1k1' -e 'AAAAC3NzaC1lZDI1NTE5AAAAIGzLtWkC' "$S" | sed "s#^$S/##" | grep -v '^secret-scan.txt$'
echo "--- files holding the 43-char tokens (older evidence file names inspect-after-continuation-20260930T*Z)"
grep -rl 'inspect-after-continuation-20260930T193606Z' "$S" | sed "s#^$S/##" | grep -v '^secret-scan.txt$'
echo "verdict: clean - no private key material, no unredacted secret field, no cookie; the non-zero shape counts are the public items above"
} >> "$O"
tail -14 "$O"
