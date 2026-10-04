# usage: show.sh FILE... (cat with cut)
for f in "$@"; do echo "===== $f"; cut -c1-3000 "$f"; done
