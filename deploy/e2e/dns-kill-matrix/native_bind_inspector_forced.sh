#!/bin/sh
# Disposable-cell forced command. The check runs before sudo resets SSH_ORIGINAL_COMMAND.
[ "$SSH_ORIGINAL_COMMAND" = "celikpanel-bind-peer-inspect-v1" ] || exit 126
exec /usr/bin/sudo -n -- /opt/celikpanel/bin/bind-peer-inspect
