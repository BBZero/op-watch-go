#!/bin/bash
# set -e

LOGTAG="[OpWatch-Release]"
currentpath="$(pwd)"
scriptpath="$(cd "$(dirname "$0")" && pwd)"

error_exit() {
    echo ""
    echo "$LOGTAG Error! $1"
    cd $currentpath
    exit 1
}
commands=(
    "cd $scriptpath"
    "./build.sh"
    "./package.sh --release"
)

for cmd in "${commands[@]}"; do
    echo "$LOGTAG Executing: $cmd"
    eval "$cmd" || error_exit "Failed: $cmd"
done

echo ""
echo "$LOGTAG All done."
echo ""