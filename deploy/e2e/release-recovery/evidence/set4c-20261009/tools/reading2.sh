#!/bin/bash
# set4c, second reading: the rollback's command for a setting that is UNSET, on a private main.cf.
# The old code kept `strings.TrimSpace` of both streams as the value. For an unset setting postconf prints an empty
# line; with one warning after it the trimmed text is the warning line alone, which is ONE line.
# Private configuration directory only; nothing of the system is read for these answers or changed.
set -u
R=/var/tmp/cp-set4c-run
OUT=$R/reading-2
[ -e $OUT ] && { echo "refusing: $OUT exists"; exit 2; }
mkdir -p $OUT
export LC_ALL=C PATH=/usr/sbin:/usr/bin:/sbin:/bin
date -u +%FT%TZ > $OUT/when.txt
for case in unset-with-unused-parameter set-with-unused-parameter; do
  D=$R/conf2-$case; C=$OUT/$case
  mkdir -p $D $C
  cat > $D/main.cf <<'EOF'
compatibility_level = 3.6
myhostname = mail.set4c.test
mydestination = localhost
queue_directory = /var/spool/postfix
smtpd_tls_cert_file = /etc/ssl/celikpanel-mail/default.crt
smtpd_tls_key_file = /etc/ssl/celikpanel-mail/default.key
campaign_note = raised for the campaign
EOF
  : > $D/master.cf
  name=tls_server_sni_maps; [ $case = set-with-unused-parameter ] && name=smtpd_tls_cert_file
  cp $D/main.cf $C/main.cf.before.txt
  /usr/sbin/postconf -c $D -h $name > $C/read.stdout 2> $C/read.stderr; echo $? > $C/read.exit
  /usr/sbin/postconf -c $D -h $name 2>&1 | cat > $C/read.combined
  # strings.TrimSpace of the combined bytes, as the old snapshot kept it
  python3 -c "import sys;sys.stdout.write(open(sys.argv[1]).read().strip())" $C/read.combined > $C/value-as-the-old-code-kept-it.txt
  echo "lines in that value: $(python3 -c "import sys;print(len(open(sys.argv[1]).read().split(chr(10))))" $C/value-as-the-old-code-kept-it.txt)" > $C/value-lines.txt
  /usr/sbin/postconf -c $D -e "$name=$(cat $C/value-as-the-old-code-kept-it.txt)" > $C/postconf-e.stdout 2> $C/postconf-e.stderr; echo $? > $C/postconf-e.exit
  cp $D/main.cf $C/main.cf.after.txt
  diff -u $C/main.cf.before.txt $C/main.cf.after.txt > $C/main.cf.diff; echo $? > $C/main.cf.diff.exit
  /usr/sbin/postconf -c $D -h $name > $C/after.stdout 2> $C/after.stderr; echo $? > $C/after.exit
  echo "== $case ($name): lines $(cat $C/value-lines.txt); postconf -e exit $(cat $C/postconf-e.exit); main.cf changed: $([ -s $C/main.cf.diff ] && echo yes || echo no)"
  cat $C/postconf-e.stderr | cut -c1-200
  grep '^[+-][^+-]' $C/main.cf.diff | cut -c1-240
done
