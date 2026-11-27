#!/bin/bash
# media.sh BASE...: builds the installer ISO, and a payload ISO of the OS
# images BASE with the installed console added as the distro adds it;
# uploads both for vm.sh, the payload as PAYLOAD. OUT is a work directory.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
[ $# -gt 0 ] || { echo "usage: media.sh BASE..., like localhost/stornas-os:sb"; exit 2; }
out=${OUT:?set OUT to a work directory}
mkdir -p "$out"
podman build -q -t localhost/amorce:dev "$repo" >/dev/null
images=()
for base in "$@"; do
  name=$(basename "${base%:*}")
  os=localhost/$name:console
  podman build -q -t "$os" --build-arg BASE="$base" -f "$here/console.Containerfile" "$here" >/dev/null
  podman save --format oci-archive -o "$out/$name.tar" "$os"
  images+=(--image "oci-archive:/out/$name.tar")
done
podman run --rm -v "$out:/out:Z" localhost/amorce:dev \
  iso --out /out/payload.iso --payload-only "${images[@]}" | grep -E 'installs as'
for base in "$@"; do rm -f "$out/$(basename "${base%:*}").tar"; done
podman run --rm -v "$out:/out:Z" localhost/amorce:dev iso --out /out/installer.iso >/dev/null
"$here/vm.sh" upload "${PAYLOAD:-msi-payload.iso}" "$out/payload.iso"
"$here/vm.sh" upload msi-installer.iso "$out/installer.iso"
rm -f "$out/payload.iso"
echo "uploaded"
