# usage: wfile.sh FILE PATTERN MAXSEC [TAILN] -- wait until FILE matches PATTERN (or MAXSEC), then tail it
end=$(( $(date +%s) + $3 ))
while [ "$(date +%s)" -lt "$end" ]; do
  grep -qE "$2" "$1" 2>/dev/null && break
  sleep 5
done
tail -n ${4:-40} "$1"
