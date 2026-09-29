set -euo pipefail
# usage: export.sh SHORT CELL   (copies one cell's evidence into the repository evidence directory)
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch4-adoption-reboot-20260929'
ROOT=/var/tmp/cp-b4-0929
s=$1 c=$2
mkdir -p "$D"
test -d $ROOT/evidence/$s
rm -rf "$D/$s"; mkdir -p "$D/$s"
(cd $ROOT/evidence/$s && tar -cf - --exclude=./raw/results .) | tar -C "$D/$s" --no-same-owner --no-same-permissions -xf -
mkdir -p "$D/$s/raw/results"
if [ -d $ROOT/evidence/$s/raw/results/$c ]; then
  (cd $ROOT/evidence/$s/raw/results/$c && tar -cf - .) | tar -C "$D/$s/raw/results" --no-same-owner --no-same-permissions -xf -
fi
find "$D/$s" -name manifest.json
echo "files: $(find "$D/$s" -type f | wc -l)"
echo "longest windows path (chars, /mnt/c/ -> C:\\): $(( $(find "$D/$s" -type f | awk '{print length($0)}' | sort -n | tail -1) - 4 ))"
find "$D/$s" -type f | awk '{print length($0), $0}' | sort -n | tail -1
du -sh "$D/$s"
