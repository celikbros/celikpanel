#!/bin/bash
cd /var/tmp/cp-set4c-run/reading/restore || exit 2
echo "-- value as the old code read it"; cat -A value-as-the-old-code-read-it.txt | cut -c1-200
echo "-- postconf -e exit $(cat postconf-e.exit)"; cat postconf-e.stdout; cat postconf-e.stderr | cut -c1-220
echo "-- diff"; cat main.cf.diff
echo "-- after -h"; echo "exit $(cat after-h-smtpd_tls_cert_file.exit)"; cat -A after-h-smtpd_tls_cert_file.stdout | cut -c1-200; cat after-h-smtpd_tls_cert_file.stderr | cut -c1-240
