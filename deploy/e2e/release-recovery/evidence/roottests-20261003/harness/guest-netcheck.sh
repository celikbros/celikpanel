# Prove the guest has no outbound network after the restrict=on restart.
date -u +%FT%TZ
ip -4 addr show mgmt0 | grep inet || true
ip route || true
if timeout 10 curl -sS -o /dev/null --connect-timeout 5 http://1.1.1.1/ 2>/tmp/nc.err; then
    echo "NETCHECK: outbound IP reachable (UNEXPECTED)"; exit 1
else
    echo "NETCHECK: outbound IP 1.1.1.1:80 unreachable: $(head -c 200 /tmp/nc.err)"
fi
if timeout 10 getent hosts celikpanel.net; then
    echo "NETCHECK: celikpanel.net resolves (resolution only; connection is still blocked)"
else
    echo "NETCHECK: celikpanel.net does not resolve"
fi
if timeout 10 curl -sS -o /dev/null --connect-timeout 5 http://10.0.2.2/ 2>/tmp/nc.err; then
    echo "NETCHECK: host gateway reachable (UNEXPECTED)"; exit 1
else
    echo "NETCHECK: host gateway 10.0.2.2:80 unreachable: $(head -c 200 /tmp/nc.err)"
fi
echo NETCHECK-DONE
