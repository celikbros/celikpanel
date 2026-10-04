E=/var/tmp/cp-b10-1001/evidence/z04-resume
grep -E '2026-09-30T02:5|2026-09-30T03:' $E/pdns-notify-journal-and-also-notify.txt | cut -c1-260
grep -E '^matching lines|also-notify=' $E/pdns-notify-journal-and-also-notify.txt
echo "this-boot notify lines: $(grep -cE '2026-09-30T0(2:5|3:)' $E/pdns-notify-journal-and-also-notify.txt)"
echo "this-boot ':0 failed after retries': $(grep -E '2026-09-30T0(2:5|3:)' $E/pdns-notify-journal-and-also-notify.txt | grep -c ':0 failed after retries')"
echo "this-boot spurious: $(grep -E '2026-09-30T0(2:5|3:)' $E/pdns-notify-journal-and-also-notify.txt | grep -ci 'spurious')"
echo "== catalog serial history"; cut -c1-250 $E/catalog-serial-history.txt | tail -n 25
echo "== CATALOG-HASH lines in pdns journal this boot"; grep -E '2026-09-30T0(2:5|3:)' $E/raw/journald/pdns.service.txt | grep -i 'catalog-hash' | cut -c1-250
grep -c . $E/peer-dns-sampler.log
