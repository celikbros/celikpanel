#!/bin/bash
set -euo pipefail
test "$(cat /etc/hostname)" = profile65-__PROFILE__
test ! -e /opt/celikpanel
test ! -e /etc/celikpanel
test ! -e /var/lib/celikpanel-profile-vm
export DEBIAN_FRONTEND=noninteractive
printf 'Dpkg::Options { "--force-confold"; };\n' > /etc/apt/apt.conf.d/99cp-fixture-confold
apt-get update
apt-get install -y dnsmasq-base ca-certificates openssl curl bind9-dnsutils
install -d -m 0700 /var/lib/celikpanel-profile-vm
printf 'fresh-setup-profile-acceptance-v1\n' > /var/lib/celikpanel-profile-vm/fixture
install -m 0755 /tmp/pebble /var/lib/celikpanel-profile-vm/pebble
cd /var/lib/celikpanel-profile-vm
openssl req -x509 -newkey rsa:2048 -nodes -days 30 -subj '/CN=CelikPanel isolated profile API CA' -keyout api-ca.key -out api-ca.crt
openssl req -newkey rsa:2048 -nodes -subj '/CN=acme.setup.test' -keyout api.key -out api.csr
printf 'subjectAltName=DNS:acme.setup.test,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' > api.ext
openssl x509 -req -in api.csr -CA api-ca.crt -CAkey api-ca.key -CAcreateserial -days 30 -extfile api.ext -out api.crt
chmod 0600 *.key
install -m 0644 api-ca.crt /usr/local/share/ca-certificates/celikpanel-fixture-api.crt
update-ca-certificates
cat > dnsmasq.conf <<'EOF'
port=5353
listen-address=127.0.0.1
bind-interfaces
no-resolv
no-hosts
server=10.0.2.3
local=/setup.test/
address=/acme.setup.test/10.0.2.15
address=/mail.__PROFILE__.setup.test/10.0.2.15
address=/panel.__PROFILE__.setup.test/__PEER_IP__
local=/ns1.setup.test/
address=/ns1.setup.test/192.0.2.10
local=/ns2.setup.test/
address=/ns2.setup.test/192.0.2.20
ptr-record=15.2.0.10.in-addr.arpa,mail.__PROFILE__.setup.test
address=/gmail-smtp-in.l.google.com/10.0.2.15
EOF
cat > /etc/systemd/system/celikpanel-fixture-dns.service <<'EOF'
[Unit]
Description=Isolated setup fixture resolver
[Service]
ExecStart=/usr/sbin/dnsmasq --no-daemon --conf-file=/var/lib/celikpanel-profile-vm/dnsmasq.conf
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF
cat > pebble.json <<'EOF'
{"pebble":{"listenAddress":"0.0.0.0:14000","managementListenAddress":"127.0.0.1:15000","certificate":"/var/lib/celikpanel-profile-vm/api.crt","privateKey":"/var/lib/celikpanel-profile-vm/api.key","httpPort":80,"tlsPort":443,"ocspResponderURL":"","externalAccountBindingRequired":false}}
EOF
cat > /etc/systemd/system/celikpanel-fixture-acme.service <<'EOF'
[Unit]
Description=Isolated ACME with real challenge verification
After=celikpanel-fixture-dns.service
[Service]
ExecStart=/var/lib/celikpanel-profile-vm/pebble -config /var/lib/celikpanel-profile-vm/pebble.json -dnsserver 127.0.0.1:5353 -strict=false
Environment=PEBBLE_VA_NOSLEEP=1 PEBBLE_WFE_NONCEREJECT=0 PEBBLE_AUTHZREUSE=0
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now celikpanel-fixture-dns.service celikpanel-fixture-acme.service
# Only the disposable guest trust store and resolver are modified.
cp -L /etc/resolv.conf /var/lib/celikpanel-profile-vm/resolv.before
printf 'nameserver 10.0.2.3\n' > /etc/resolv.conf
printf '10.0.2.15 acme.setup.test\n__PEER_IP__ panel.__PROFILE__.setup.test\n' >> /etc/hosts
cp /etc/nsswitch.conf /var/lib/celikpanel-profile-vm/nsswitch.before
sed -i 's/^hosts:.*/hosts: files dns myhostname/' /etc/nsswitch.conf
for attempt in $(seq 1 30); do
  if curl --silent --show-error --fail https://127.0.0.1:15000/roots/0 > issued-root.crt; then break; fi
  sleep 1
done
openssl x509 -in issued-root.crt -noout -subject
install -m 0644 issued-root.crt /usr/local/share/ca-certificates/celikpanel-fixture-issuance.crt
update-ca-certificates
install -d -m 0700 /etc/letsencrypt
printf 'server = https://acme.setup.test:14000/dir\n' > /etc/letsencrypt/cli.ini
curl --silent --show-error --fail https://acme.setup.test:14000/dir > acme-directory.json
getent ahostsv4 panel.__PROFILE__.setup.test
systemctl is-active celikpanel-fixture-acme.service celikpanel-fixture-dns.service
printf 'PROFILE_INFRA_READY __PROFILE__\n'
