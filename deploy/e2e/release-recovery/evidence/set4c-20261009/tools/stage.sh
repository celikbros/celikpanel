#!/bin/bash
# set4c: copy the two readings and the tools into the repository's evidence folder, verify the copy file by file,
# run the secret scan and write SHA256SUMS last. Nothing on the guest is removed.
set -uo pipefail
R=/var/tmp/cp-set4c-run
E='/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set4c-20261009'
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
mkdir -p "$E/tools" "$E/verification"
for d in reading reading-2; do
  [ -e "$E/$d" ] || cp -r $R/$d "$E/$d"
  ( cd $R/$d && find . -type f -print0 | sort -z | xargs -0 sha256sum ) > $R/$d.sha256
  ( cd "$E/$d" && sha256sum -c --quiet $R/$d.sha256 ) && echo "$d: $(wc -l < $R/$d.sha256) files equal their source"
done
for f in reading.sh reading2.sh show.sh show2.sh stage.sh go-quick.sh go-v.sh go-final.sh failset.sh secretscan.py docs_gen_c.py; do
  [ -f "$J/$f" ] && sed "s#/mnt/c/Users/[^/]*/AppData/Local/Temp/claude/[^/]*/[^/]*/scratchpad#<scratchpad>#g" "$J/$f" > "$E/tools/$f"
done
for f in go-v.log go-final-id.txt go-final-gofmt.txt go-final-vet.txt go-final-build-amd64.txt go-final-build-arm64.txt go-final-summary.txt \
         go-base-agent-failset.txt go-final-agent-failset.txt go-final-other-failset.txt web-test-final.log; do
  [ -f "$J/$f" ] && tr -d '\015' < "$J/$f" > "$E/verification/$f.txt"
done
[ -f $J/README.md ] && tr -d '\015' < $J/README.md > "$E/README.md"
rm -f "$E/SHA256SUMS" "$E/secret-scan.txt"
python3 $J/secretscan.py "$E" > $R/secret-scan.txt 2> $R/secret-scan.err; echo "scan rc=$?"; tail -n 2 $R/secret-scan.err
cp $R/secret-scan.txt "$E/secret-scan.txt"; tail -n 1 "$E/secret-scan.txt"
PROFILE='/Users/[A-Za-z0-9._-]+/AppData|@gmail\.com'
echo "files naming a local user profile or a mail address: $(grep -rIl -E "$PROFILE" "$E" | wc -l)"
( cd "$E" && find . -type f ! -path ./SHA256SUMS -print0 | sort -z | xargs -0 sha256sum | sed 's#  \./#  #' > SHA256SUMS && sha256sum -c --quiet SHA256SUMS && echo "SHA256SUMS verified: $(wc -l < SHA256SUMS) files" )
du -sh "$E" | cut -f1
