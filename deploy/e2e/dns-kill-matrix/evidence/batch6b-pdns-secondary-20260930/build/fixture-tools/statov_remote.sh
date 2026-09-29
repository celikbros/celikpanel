echo "wall=$(date -u +%FT%T.%NZ)"
echo "== dpkg-statoverride --list (read-only)"; dpkg-statoverride --list 2>&1; echo "rc=$?"
echo "== dpkg-statoverride --list /var/cache/bind (read-only)"; dpkg-statoverride --list /var/cache/bind 2>&1; echo "rc=$?"
echo "== dpkg-query -S /var/cache/bind"; dpkg-query -S /var/cache/bind 2>&1; echo "rc=$?"
