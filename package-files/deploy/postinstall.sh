#!/bin/bash
set -e

SERVICE_NAME="opwatch"
ACTION="$1"
OLD_VERSION="$2"

echo "[postinst] action=${ACTION}, old_version=${OLD_VERSION}" >&2

# Reload systemd to pick up new/changed service file
systemctl daemon-reload || true

if [ "${ACTION}" = "configure" ]; then
    echo "[postinst] Reload systemd and enable $SERVICE_NAME..." >&2
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl enable $SERVICE_NAME.service >/dev/null 2>&1 || true
    if [ -z "${OLD_VERSION}" ]; then
        # Fresh install: enable on boot but do NOT start
        # User should edit /etc/opwatch/watch.conf first, then manually start
        # systemctl enable "${SERVICE_NAME}" 2>/dev/null || true
        # echo "[postinst] Fresh install: service enabled but NOT started." >&2
        echo "[postinst] Please edit /etc/opwatch/watch.conf, then run:" >&2
        echo "[postinst]   sudo systemctl start ${SERVICE_NAME}" >&2
    else
        systemctl restart "${SERVICE_NAME}" || true
        echo "[postinst] Upgrade from ${OLD_VERSION} and restart service." >&2
    fi
fi