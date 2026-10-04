set -uo pipefail
exec > /var/tmp/cp-b10-webbuild.log 2>&1
date -u +%FT%TZ
cd /root/cp-b10-src/web
test ! -e dist
node --version; npm --version
npm ci --no-audit --no-fund && npm run build
rc=$?
echo "web build rc=$rc"
if [ $rc -eq 0 ]; then ( cd dist && find . -type f ! -path './.vite/*' -print0 | sort -z | xargs -0 sha256sum ) > /var/tmp/cp-b10-webdist.sha256; wc -l /var/tmp/cp-b10-webdist.sha256; fi
rm -rf node_modules
date -u +%FT%TZ
echo WEB-DONE rc=$rc
