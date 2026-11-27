#!/bin/bash
# cycle.sh VM PORT [fresh]: rebuild the image and ISO, then boot VM from it.
# With fresh, the VM's disks are recreated first.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
out=${OUT:?set OUT to a work directory}
vm=$1 port=$2
podman build -q -t localhost/amorce:dev "$repo" >/dev/null
podman run --rm -v "$out:/out:Z" localhost/amorce:dev iso --out /out/installer.iso >/dev/null
export LIBVIRT_DEFAULT_URI=qemu:///system
kill "$(cat "$STATE/$vm/bridge.pid" 2>/dev/null)" 2>/dev/null || true
if [ "${3:-}" = fresh ]; then
  "$here/vm.sh" destroy "$vm"
  "$here/vm.sh" upload msi-installer.iso "$out/installer.iso"
  "$here/vm.sh" create "$vm" "$port"
else
  virsh -q destroy "$vm" 2>/dev/null || true
  "$here/vm.sh" upload msi-installer.iso "$out/installer.iso"
  virsh -q start "$vm"
fi
: > "$STATE/$vm/serial.log" 2>/dev/null || true
"$here/vm.sh" bridge "$vm" "$port" "${COLS:-100}" "${ROWS:-30}"
echo "booted $vm"
