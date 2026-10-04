# usage: show.sh FILE... (cat with cut)
for f in "$@"; do echo "===== $f"; cut -c1-4000 "$f"; done
