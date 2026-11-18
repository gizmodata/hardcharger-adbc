#!/usr/bin/env bash
# Package the built library (go/build/) as a release archive:
#   dist/libadbc_driver_hardcharger-<ref>-<platform>.tar.gz
# holding the library, the manifest template, LICENSE and THIRD_PARTY_NOTICES in a folder of the
# same name (the layout the ADBC driver installer, github.com/gizmodata/adbc-install, unpacks;
# release.yml's release-files job checks it with scripts/release_files.sh).
#
#   scripts/package_lib.sh <platform> <ref>    # e.g. linux_amd64 v2.6.1
set -euo pipefail
cd "$(dirname "$0")/.."

[ $# -eq 2 ] || { echo "usage: $0 <platform> <ref>" >&2; exit 2; }
platform=$1
ref=${2//\//-}  # a branch name may hold slashes
case "$platform" in
  linux_*) ext=so ;;
  macos_*) ext=dylib ;;
  windows_*) ext=dll ;;
  *) echo "unknown platform $platform" >&2; exit 2 ;;
esac

pkg="libadbc_driver_hardcharger-$ref-$platform"
rm -rf "dist/$pkg"
mkdir -p "dist/$pkg"
cp "go/build/libadbc_driver_hardcharger.$ext" packaging/hardcharger.toml.in LICENSE \
  python/src/adbc_driver_hardcharger/THIRD_PARTY_NOTICES "dist/$pkg/"
tar -C dist -czf "dist/$pkg.tar.gz" "$pkg"
echo "dist/$pkg.tar.gz"
