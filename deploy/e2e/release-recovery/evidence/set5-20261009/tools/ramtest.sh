#!/bin/bash
# set5 read-mostly probe: kernel, memory, and whether a RAM-backed (tmpfs) file accepts O_DIRECT writes as QEMU's cache=none needs
uname -r; free -m | sed -n 1,3p
T=/var/tmp/cp-set5-run/ramtest
mkdir -p "$T" && mount -t tmpfs -o size=64m,mode=0700 tmpfs "$T" && echo mounted
dd if=/dev/zero of="$T/probe.bin" bs=1M count=4 oflag=direct 2>&1 | tail -n 1; echo "dd direct write rc=$?"
dd if="$T/probe.bin" of=/dev/null bs=1M iflag=direct 2>&1 | tail -n 1; echo "dd direct read rc=$?"
qemu-img create -f qcow2 "$T/t.qcow2" 16M > /dev/null && qemu-io -t none -c "write 0 1M" "$T/t.qcow2" 2>&1 | tail -n 2; echo "qemu-io cache=none rc=$?"
umount "$T" && rmdir "$T" && echo unmounted
findmnt -t tmpfs -n -o TARGET,SIZE,USED | head -8
qemu-system-x86_64 --version | head -1
