#!/bin/bash
# set4c: what the real `postconf` writes to each stream when main.cf makes it warn. A private configuration
# directory only (`postconf -c DIR`): the system's Postfix configuration is neither read for these answers nor
# changed, no service is touched, nothing is contacted. Run as root in the development guest that has Postfix.
# Output: one folder per case under OUT with, for every command, its stdout, stderr, both streams in the one pipe a
# caller of CombinedOutput reads, and the exit status.
set -u
R=/var/tmp/cp-set4c-run
OUT=$R/reading
[ -e $OUT ] && { echo "refusing: $OUT exists"; exit 2; }
mkdir -p $OUT
export LC_ALL=C PATH=/usr/sbin:/usr/bin:/sbin:/bin
{
  date -u +%FT%TZ
  sed -n 's/^PRETTY_NAME=//p' /etc/os-release
  uname -r
  dpkg-query -W -f '${Package} ${Version} ${db:Status-Abbrev}\n' postfix 2>&1
  command -v postconf
  postconf -d -h mail_version 2>&1
  echo "postfix service units known to systemd here: $(systemctl list-unit-files 'postfix*' --no-legend 2>/dev/null | wc -l)"
} > $OUT/platform.txt

base() {  # a small main.cf of the lab's own; no file of the system is copied
  cat <<'EOF'
compatibility_level = 3.6
myhostname = mail.set4c.test
mydestination = localhost
queue_directory = /var/spool/postfix
alias_maps = hash:/etc/aliases
alias_database = $alias_maps
smtpd_tls_cert_file = /etc/ssl/celikpanel-mail/default.crt
smtpd_tls_key_file = /etc/ssl/celikpanel-mail/default.key
smtpd_tls_security_level = may
EOF
}

one() {  # one command, each stream kept
  local dir=$1 name=$2; shift 2
  "$@" > $dir/$name.stdout 2> $dir/$name.stderr; echo $? > $dir/$name.exit
  "$@" 2>&1 | cat > $dir/$name.combined
  printf '%s\n' "$*" > $dir/$name.argv
}

for case in clean comment unused both; do
  D=$R/conf-$case; C=$OUT/$case
  mkdir -p $D $C
  base > $D/main.cf
  [ -f /etc/postfix/master.cf ] && : > $D/master.cf
  case $case in
    comment) echo 'default_process_limit = 200 # raised for the campaign' >> $D/main.cf;;
    unused)  echo 'campaign_note = raised for the campaign' >> $D/main.cf;;
    both)    echo 'default_process_limit = 200 # raised for the campaign' >> $D/main.cf; echo 'campaign_note = raised for the campaign' >> $D/main.cf;;
  esac
  cp $D/main.cf $C/main.cf.txt
  for p in postconf /usr/sbin/postconf; do
    t=$(basename $(dirname $p)); [ $p = postconf ] && t=bare || t=path
    one $C $t-h-smtpd_tls_cert_file $p -c $D -h smtpd_tls_cert_file
    one $C $t-h-tls_server_sni_maps $p -c $D -h tls_server_sni_maps
    one $C $t-h-queue_directory $p -c $D -h queue_directory
    one $C $t-xh-alias_database $p -c $D -x -h alias_database
    one $C $t-d-mail_version $p -c $D -d mail_version
    one $C $t-n $p -c $D -n
  done
done

# What the rollback's command writes when the value it restores is the text of both streams (the warning line, a line
# break, the value), on a private main.cf: `postconf -e name=<that text>`.
D=$R/conf-restore; C=$OUT/restore
mkdir -p $D $C
base > $D/main.cf; echo 'campaign_note = raised for the campaign' >> $D/main.cf
cp $D/main.cf $C/main.cf.before.txt
read_as_before=$(/usr/sbin/postconf -c $D -h smtpd_tls_cert_file 2>&1)
printf '%s\n' "$read_as_before" > $C/value-as-the-old-code-read-it.txt
/usr/sbin/postconf -c $D -e "smtpd_tls_cert_file=$read_as_before" > $C/postconf-e.stdout 2> $C/postconf-e.stderr; echo $? > $C/postconf-e.exit
cp $D/main.cf $C/main.cf.after.txt
diff -u $C/main.cf.before.txt $C/main.cf.after.txt > $C/main.cf.diff; echo $? > $C/main.cf.diff.exit
one $C after-h-smtpd_tls_cert_file /usr/sbin/postconf -c $D -h smtpd_tls_cert_file
one $C after-n /usr/sbin/postconf -c $D -n
( cd $OUT && find . -type f | sort | wc -l )
