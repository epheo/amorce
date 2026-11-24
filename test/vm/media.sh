#!/bin/bash
# media.sh BASE: builds the installer ISO, and a payload ISO of the OS image
# BASE with the installed console added as the distro adds it; uploads both
# for vm.sh, the payload as PAYLOAD. OUT is a work directory.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
base=${1:?an OS image, like localhost/stornas-os:sb}
out=${OUT:?set OUT to a work directory}
mkdir -p "$out"
podman build -q -t localhost/microshift-installer:dev "$repo" >/dev/null
podman build -q -t localhost/stornas-os:console --build-arg BASE="$base" -f "$here/console.Containerfile" "$here" >/dev/null
podman save --format oci-archive -o "$out/console.tar" localhost/stornas-os:console
podman run --rm -v "$out:/out:Z" localhost/microshift-installer:dev \
  iso --out /out/payload.iso --payload-only --image oci-archive:/out/console.tar | grep -E 'installs as'
rm -f "$out/console.tar"
podman run --rm -v "$out:/out:Z" localhost/microshift-installer:dev iso --out /out/installer.iso >/dev/null
"$here/vm.sh" upload "${PAYLOAD:-msi-payload.iso}" "$out/payload.iso"
"$here/vm.sh" upload msi-installer.iso "$out/installer.iso"
rm -f "$out/payload.iso"
echo "uploaded"
