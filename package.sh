#!/bin/bash
# set -e
LOGTAG="[OpWatch-Pkg]"
show_usage() {
    echo "$LOGTAG Usage option"
    echo "  --release  Package for release"
    #echo "  -h, --help Show help"
    echo ""
}

FOR_RELEASE=0
case "$1" in
    --release)
	echo "$LOGTAG package for release true"
    FOR_RELEASE=1
	#will go to below
        ;;
    "")
	#no parameter
	echo "$LOGTAG No parameter"
	#will go to the below
	;;
    *)
        echo "$LOGTAG Error: unknown parameter '$1'" >&2
        show_usage
        exit 1
        ;;
esac


currentpath="$(pwd)"
scriptpath="$(cd "$(dirname "$0")" && pwd)"
build_out="$scriptpath/output"
binpath="$build_out/opwatch"
pkgfiles="$scriptpath/package-files"
pkgout_root="$scriptpath/package-out"
pkgprepare="$pkgout_root/pkgprepare"
versionfile="$build_out/version.txt"
nfpm_bin="$HOME/go/bin/nfpm"
doc_path="$scriptpath/doc"

project_name="opwatch"

echo "$LOGTAG script call path : $currentpath"
echo "$LOGTAG script path : $scriptpath"

if [ ! -f "$nfpm_bin" ]; then
    echo "$LOGTAG nfpm not found."
    printf "Install nfpm? (y/n): "
    read answer
    if [ "$answer" = "y" ]; then
        echo "$LOGTAG installing nfpm..."
        go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
    else
        echo "$LOGTAG user abort."
        exit 1
    fi
fi

echo "$LOGTAG checking binary..."
if [ ! -f "$binpath" ]; then
    echo "$LOGTAG Error: $binpath not found, run build.sh first"
    exit 1
fi

VERSION=$(cat "$versionfile" 2>/dev/null || echo "0.0.0")
PACKAGE_NAME="${project_name}_${VERSION}_$(date +%Y%m%d)_amd64.deb" #-%H%M

echo "$LOGTAG preparing package files(will delete old files)..."
rm -rf "$pkgprepare"
rm -rf "$pkgout_root"
mkdir -p "$pkgprepare/app"
cp -r "$pkgfiles"/* "$pkgprepare/"
cp -r "$build_out"/* "$pkgprepare/app/"

if [ -f "$pkgprepare/nfpm.yaml" ]; then
    sed -i "s/version: .*/version: $VERSION/" "$pkgprepare/nfpm.yaml"
fi

echo "$LOGTAG packaging..."
cd "$pkgprepare"
$nfpm_bin package --packager deb --target "$pkgout_root/$PACKAGE_NAME" || {
    echo "$LOGTAG pack failed"
    cd "$currentpath"
    exit 1
}

cd "$currentpath"
echo ""
echo "$LOGTAG pack success: $pkgout_root/$PACKAGE_NAME"
echo ""

# package for release
if [ $FOR_RELEASE -eq 0 ]; then
    echo "$LOGTAG =========== Exit without package for release ==========="
    exit 0
else
    echo "$LOGTAG =========== Continue package for release ==========="
fi

RELEASE_OUT_ROOT=$pkgout_root/$project_name-$VERSION
if [ -d $RELEASE_OUT_ROOT ]; then
     echo "$LOGTAG $RELEASE_OUT_ROOT exists, will remove it"
     rm -rf $RELEASE_OUT_ROOT
fi
mkdir -p $RELEASE_OUT_ROOT

echo "$LOGTAG Move deb file"
mv "$pkgout_root/$PACKAGE_NAME" "$RELEASE_OUT_ROOT"

echo "$LOGTAG Copy doc"
cp -r "$doc_path/usage.md" "$RELEASE_OUT_ROOT/" || echo "$LOGTAG Error: copy usage.md failed"

git_sha1="unknown_sha1"
if command -v git >/dev/null 2>&1; then
    if git_sha1=$(git rev-parse HEAD 2>/dev/null); then
        :
    else
        echo "$LOGTAG no git command"
        git_sha1="unknown_sha1"
    fi
fi
compile_time=$(date +"%Y-%m-%d %H:%M:%S")
echo "compile time: $compile_time" > "$RELEASE_OUT_ROOT/$git_sha1"

echo "$LOGTAG package for release success: $RELEASE_OUT_ROOT"
