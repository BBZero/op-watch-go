#!/bin/bash
set -e
echo "[postrm] Parameter: $1, $2" >&2

if [ "$1" = "purge" ]; then
    echo "[postrm] purge and delete many" >&2

    # Debian style: --system only deletes actual system users/groups (sanity check)
    deluser --system opwatch \
        || echo "[postrm] WARNING: user opwatch kept (not a system user or still in use)." >&2
    delgroup --system opwatch || true

    rm -f /var/log/opwatch/opwatch.log || true
    rm -rf /var/log/opwatch/ || true
    rm -rf /etc/opwatch || true
    rm -rf /var/lib/opwatch || true
fi

systemctl daemon-reload || true
