cd /var/tmp/cp-b8r-1001/evidence
for s in $(ls); do echo "== $s"; grep -hE '^os=|^(pdns-server|bind9|systemd) |^(bind|powerdns|linux|systemd) ' $s/versions-post-collect-guest.txt | tr '\n' ';'; echo; grep -hE '^os=|^(bind|powerdns) ' $s/versions-post-collect-other.txt | tr '\n' ';'; echo; done
