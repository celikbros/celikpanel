# Renames the retained V3 journal archive copies (names over 200 characters as repository paths) to
# v3-archive-<request id>.json and records original name, new name and SHA-256 in raw/state/renamed-files.txt.
D='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-kill-matrix/evidence/batch8-pdns-primary-20260930'
for f in "$D"/*/raw/state/dns-engine-switch-v3-archive-*.json; do
  [ -e "$f" ] || continue
  dir=$(dirname "$f"); base=$(basename "$f")
  rid=$(echo "$base" | sed -E 's/^dns-engine-switch-v3-archive-([0-9a-f]{32})-[0-9a-f]{64}\.json$/\1/')
  new="v3-archive-$rid.json"
  test ! -e "$dir/$new"
  sum=$(sha256sum "$f" | cut -c1-64)
  mv "$f" "$dir/$new"
  echo "original=$base renamed=$new sha256=$sum (renamed only to keep the repository path under 200 characters; bytes unchanged)" >> "$dir/renamed-files.txt"
  echo "$dir/$new"
done
cd '/mnt/c/CELIKBROS PROJECTS/celikpanel'
echo "longest repo-relative path:"; find deploy/e2e/dns-kill-matrix/evidence/batch8-pdns-primary-20260930 -type f | awk '{print length($0), $0}' | sort -n | tail -1
echo "longest Windows path length: $(( $(find deploy/e2e/dns-kill-matrix/evidence/batch8-pdns-primary-20260930 -type f | awk '{print length($0)}' | sort -n | tail -1) + 33 ))"
