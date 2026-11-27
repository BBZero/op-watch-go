#!/bin/bash
set -e
echo "[prerm] Parameter: $1, $2" >&2

SERVICE_NAME="opwatch"
    
# purge and remove will both be remove
case "$1" in
	upgrade)
		systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
		;;
	remove)
        echo "[prerm] Stop and disable service." >&2
		systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
		systemctl disable "${SERVICE_NAME}" 2>/dev/null || true
		;;
esac
