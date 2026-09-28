#!/bin/sh
# Disposable peer only: compare the original SSH command before sudo clears it.
[ "$SSH_ORIGINAL_COMMAND" = "celikpanel-pdns-peer-inspect-v1" ] || exit 126
exec /usr/bin/sudo -n -- /opt/celikpanel/bin/pdns-peer-inspect
