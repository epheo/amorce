#!/bin/bash
# build.sh [TAG]: stornas-os, with the pins of versions.env; any of them set
# non-empty in the environment wins. STORNAS_SRC, a stornas checkout, builds
# from it instead: its image/manifests/stornas, and its components from the
# PODMAN storage its make images used. Run it as the user who built them:
# sudo drops the environment.
set -euo pipefail
if [ -n "${STORNAS_SRC:-}" ]; then
    STORNAS_SRC=$(realpath "$STORNAS_SRC")
fi
PODMAN=${PODMAN:-podman}
cd "$(dirname "$0")"
eval "$(sed -n 's/^\([A-Z_]*\)=\(.*\)$/: "${\1:=\2}"/p' versions.env)"
echo "base: $BASE_IMAGE"
rm -rf build
mkdir -p build/embedded-images
if [ -n "${STORNAS_SRC:-}" ]; then
    cp -r "$STORNAS_SRC/image/manifests/stornas" build/manifests
    rev=dev
else
    [ -n "$STORNAS_REV" ] || { echo "no STORNAS_REV, and no STORNAS_SRC" >&2; exit 1; }
    # The commit names the manifests' content; git checks it on fetch.
    git init -q build/stornas
    git -C build/stornas fetch -q --depth 1 https://github.com/epheo/stornas "$STORNAS_REV"
    git -C build/stornas checkout -q FETCH_HEAD -- image/manifests/stornas
    mv build/stornas/image/manifests/stornas build/manifests
    rm -rf build/stornas
    rev=$STORNAS_REV
fi
# The manifests run each component as ghcr.io/epheo/<name>:latest with
# imagePullPolicy Never, so that is the name the archive carries.
for name in stornas stornas-operator stornas-agent; do
    var=$(echo "${name}_image" | tr a-z- A-Z_)
    if [ -n "${STORNAS_SRC:-}" ]; then
        $PODMAN save -q --format oci-archive -o "build/embedded-images/$name.tar" "ghcr.io/epheo/$name:latest"
    else
        [ -n "${!var}" ] || { echo "no $var" >&2; exit 1; }
        skopeo copy -q "docker://${!var}" "oci-archive:build/embedded-images/$name.tar:ghcr.io/epheo/$name:latest"
    fi
    echo "/usr/lib/embedded-images/$name.tar ghcr.io/epheo/$name:latest" >>build/embedded-images/manifest.stornas
done
$PODMAN build -t "${1:-localhost/stornas-os:dev}" -f Containerfile \
    --build-arg BASE_IMAGE="$BASE_IMAGE" \
    --label org.opencontainers.image.base.name="$BASE_IMAGE" \
    --label org.opencontainers.image.source=https://github.com/epheo/amorce \
    --label org.opencontainers.image.revision="$rev" .
