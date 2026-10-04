( cd /root/cp-b8r-src/web/dist && find . -type f ! -path './.vite/*' -print0 | sort -z | xargs -0 sha256sum ) > /var/tmp/cp-b8r-webdist.sha256
echo "web dist hashed after bundle-budget exit 1 (dist produced; used as in batch 8)" >> /var/tmp/cp-b8r-webbuild.log
wc -l /var/tmp/cp-b8r-webdist.sha256
ls -la /root/cp-b8r-src/web | head -30
