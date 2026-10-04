# usage: waitfile.sh PATTERN FILE [SECONDS] -- wait until FILE contains PATTERN, then tail it
end=$(( $(date +%s) + ${3:-570} ))
while [ "$(date +%s)" -lt "$end" ]; do grep -qE "$1" "$2" 2>/dev/null && break; sleep 5; done
date -u +%FT%TZ; tail -n ${4:-30} "$2" | cut -c1-300
