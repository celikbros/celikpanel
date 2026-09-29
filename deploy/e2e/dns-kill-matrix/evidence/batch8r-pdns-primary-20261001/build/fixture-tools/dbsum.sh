# Read-only: PowerDNS database summary at the end, catalog serial history and secondary state per cell
while read s c; do
  E=/var/tmp/cp-b8r-1001/evidence/$s
  echo "==== $s"
  grep -E '^== |-- table (domains|records|domainmetadata|comments|cryptokeys|tsigkeys|supermasters)|records per domain:|   domains:|   domainmetadata:' $E/pdns-db-post-collect.txt 2>/dev/null | cut -c1-330
  echo "-- serial history"; grep -o '"catalog_soa_serial": "[0-9]*"[^}]*"catalog_metadata": [^]]*\]*\]' $E/catalog-serial-history.txt 2>/dev/null | cut -c1-200 | uniq
  grep -c 'copy' $E/catalog-serial-history.txt 2>/dev/null
  echo "-- secondary"; grep -A2 'catalog members' $E/bind-secondary-state-post-collect.txt | tail -2 | cut -c1-200
  grep -E 'Transfer completed' $E/bind-secondary-state-post-collect.txt | cut -c1-260 | tail -3
  echo "-- sampler lines $(grep -vc '^####' $E/peer-dns-sampler.log)"; grep -v '^####' $E/peer-dns-sampler.log | tail -1 | cut -c1-400
done < /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch8r/cells.txt
