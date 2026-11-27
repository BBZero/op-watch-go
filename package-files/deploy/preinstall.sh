#!/bin/bash
set -e

echo "[preinst] Parameter: $1, $2" >&2

SERVICE_USER="opwatch"
HOME_DIR="/var/lib/opwatch"

# Debian style: adduser --system --group creates user + same-name group in one go
# (idempotent for upgrades)
if ! getent passwd "${SERVICE_USER}" >/dev/null 2>&1; then
    adduser --system \
        --group \
        --home "${HOME_DIR}" \
        --no-create-home \
        --shell /usr/sbin/nologin \
        --gecos "Op Watch service account" \
        "${SERVICE_USER}"
    echo "[preinst] Created system user ${SERVICE_USER}." >&2
else
    echo "[preinst] User ${SERVICE_USER} already exists, skipping." >&2
    EXISTING_SHELL="$(getent passwd "${SERVICE_USER}" | awk -F: '{print $7; exit}')"
    if [ "${EXISTING_SHELL}" != "/usr/sbin/nologin" ]; then
        echo "[preinst] WARNING: existing shell is '${EXISTING_SHELL}', not login-disabled." >&2
        echo "[preinst] WARNING: run 'usermod -s /usr/sbin/nologin ${SERVICE_USER}' to fix." >&2
    fi
fi