date -u +%FT%TZ
free -m
df -h /var/tmp /root
nproc
pgrep -af qemu-system | cut -c1-200 || echo "no qemu"
ls -d /var/tmp/cp-b12* /root/cp-b12* 2>&1
ls -d /var/tmp/cp-* /root/cp-* 2>&1
ls /var/tmp/cp-v3n28/images
ls -la /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go
ls /root/cp-b10-tools
node --version; npm --version; python3 --version
ls -la /dev/kvm
ss -ltn | grep -E ':(2201|2202|23053) ' || echo "fixture ports free"
