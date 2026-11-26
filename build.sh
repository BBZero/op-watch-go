#!/bin/bash
# set -e
LOGTAG="[OpWatch-Build]"
show_usage() {
    echo "$LOGTAG Usage option"
    echo "  --devmode  Do not overwrite conf"
    echo "  -h, --help Show help"
    echo ""
}
error_exit() {
    echo "$LOGTAG Error: $*" >&2
    exit 1
}
devbuildmode=1
case "$1" in
    --devmode)
    devbuildmode=1
	#will go to below
        ;;
    -h|--help)
        show_usage
	exit 2
        ;;
   "")
	#no parameter
	echo "$LOGTAG Run without parameter"
	;;
    *)
        echo "$LOGTAG Error: unknown parameter '$1'" >&2
        show_usage
        exit 1
        ;;
esac
echo "$LOGTAG DevbuildMode: $devbuildmode"

currentpath="$(pwd)"
scriptpath="$(cd "$(dirname "$0")" && pwd)"

srcpath="$scriptpath/src"
outpath_root="$scriptpath/output"
outbin="op-watch"

echo "$LOGTAG script call path : $currentpath"
echo "$LOGTAG script path : $scriptpath"

VERSION="1.0.0"
COMMIT_SHORT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
VERSION_PKG="github.com/BBZero/op-watch-go/version"
BUILD_ENTRY="./main"

cd $srcpath
echo "$LOGTAG tidy dependencies..."
go mod tidy
exit_status=$?
if [ $exit_status -ne 0 ]; then
  echo "$LOGTAG Error: go mod tidy fail but will continue..."
fi

if [ -d "$outpath_root" ]; then
    if [ "$devbuildmode" -gt 0 ]; then
        echo "$LOGTAG [devbuildmode] keeping old build directory: $outpath_root"
    else
        echo "$LOGTAG removing old build: $outpath_root"
        rm -rf "$outpath_root"
    fi
fi
echo "$LOGTAG building version: $VERSION ($COMMIT_SHORT) ..."
mkdir -p "$outpath_root"
# go clean -cache
# echo "$LOGTAG ldflags: -X ${VERSION_PKG}.Version=${VERSION} -X ${VERSION_PKG}.Commit=${COMMIT}"
go build -v -ldflags "-X ${VERSION_PKG}.Version=${VERSION} -X ${VERSION_PKG}.Commit=${COMMIT_SHORT}" -o "$outpath_root/$outbin" ${BUILD_ENTRY} ||{
    echo "$LOGTAG build failed"
    cd $currentpath
    exit 1
}

if [ "$devbuildmode" -gt 0 ] && [ -f "$outpath_root/watch.json" ]; then
    echo "$LOGTAG [devbuildmode] watch.json already exists, skipping copy."
else
    cp ./watch.json "$outpath_root/"
     echo "$LOGTAG watch.json copied to output directory."
fi

echo "$VERSION" > "$outpath_root/version.txt"

cd $currentpath
echo ""
echo "$LOGTAG build success: $outpath_root"
echo "$LOGTAG version: $VERSION ($COMMIT_SHORT)"
echo ""
