# Replace the CRLF-converted copy with the byte-exact LF archive; keep the first passes' results aside.
if systemctl is-active --quiet rt-unpriv || systemctl is-active --quiet rt-root; then echo busy; exit 1; fi
L=/root/celikpanel-release-recovery-lab
echo "dec4e366ad6e38081f66d20f5fced961e1a96122ce21a3b951942f6bbaa17547  $L/src-48d264d5-lf.tar" | sha256sum -c
rm -rf /srv/rt/out-attempt1-crlf && mv /srv/rt/out /srv/rt/out-attempt1-crlf
install -d -m 0755 /srv/rt/out
install -m 0644 $L/src-48d264d5-lf.tar /srv/rt/src.tar
sha256sum /srv/rt/src.tar
tar -xOf /srv/rt/src.tar deploy/recovery/agent-checker.sources | od -c | head -2
rm -rf /srv/rt/work /home/rtuser/rt
ls /tmp | head -50; ls /tmp | wc -l
