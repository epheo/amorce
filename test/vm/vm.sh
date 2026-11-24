#!/bin/bash
# Test VMs on the host's libvirt: create, attach media, watch, destroy.
set -euo pipefail
export LIBVIRT_DEFAULT_URI=qemu:///system
here=$(cd "$(dirname "$0")" && pwd)
pool=default
py=${PY:-python3}
state=${STATE:-/tmp/msi-vm}

upload() { # name file
  virsh -q vol-delete --pool $pool "$1" 2>/dev/null || true
  virsh -q vol-create-as $pool "$1" "$(stat -c %s "$2")" --format raw >/dev/null
  virsh -q vol-upload --pool $pool "$1" "$2"
}

create() { # name port [secureboot [virt-install args...]]
  local n=$1 port=$2 sb=${3:-off} d args=()
  shift $(($# < 3 ? $# : 3))
  for d in sys1:40 sys2:40 hdd1:16 hdd2:16 hdd3:16 hdd4:16; do
    virsh -q vol-delete --pool $pool "$n-${d%%:*}.qcow2" 2>/dev/null || true
    virsh -q vol-create-as $pool "$n-${d%%:*}.qcow2" "${d##*:}G" --format qcow2 >/dev/null
  done
  args+=(--disk "vol=$pool/$n-sys1.qcow2,bus=sata,target.rotation_rate=1,serial=SSD0001,boot.order=1")
  args+=(--disk "vol=$pool/$n-sys2.qcow2,bus=sata,target.rotation_rate=1,serial=SSD0002")
  for i in 1 2 3 4; do
    args+=(--disk "vol=$pool/$n-hdd$i.qcow2,bus=scsi,target.rotation_rate=7200,serial=HDD000$i")
  done
  args+=(--disk "vol=$pool/msi-installer.iso,device=cdrom,bus=sata,boot.order=2")
  # PAYLOAD picks the images medium, so tests of different images can run at once.
  if virsh -q vol-info --pool $pool "${PAYLOAD:-msi-payload.iso}" >/dev/null 2>&1; then
    args+=(--disk "vol=$pool/${PAYLOAD:-msi-payload.iso},device=cdrom,bus=sata")
  fi
  local fw="uefi,firmware.feature0.name=secure-boot,firmware.feature0.enabled=no,firmware.feature1.name=enrolled-keys,firmware.feature1.enabled=no"
  [ "$sb" = on ] && fw="uefi,firmware.feature0.name=secure-boot,firmware.feature0.enabled=yes,firmware.feature1.name=enrolled-keys,firmware.feature1.enabled=yes"
  virt-install -q --name "$n" --memory 8192 --vcpus 4 --osinfo centos-stream10 \
    --boot "$fw" --controller scsi,model=virtio-scsi "${args[@]}" \
    --network network=default,model=virtio --tpm none \
    --serial "tcp,host=127.0.0.1:$port,mode=bind,protocol.type=raw" \
    --graphics vnc,listen=127.0.0.1 --video virtio --noautoconsole "$@"
}

flatnet() { # a network with no DHCP and no gateway; the host is 10.99.0.1
  virsh -q net-info msi-flat >/dev/null 2>&1 && return
  virsh -q net-define /dev/stdin <<'XML'
<network><name>msi-flat</name><ip address="10.99.0.1" prefix="24"/></network>
XML
  virsh -q net-start msi-flat
}

destroy() { # name
  virsh -q destroy "$1" 2>/dev/null || true
  virsh -q undefine "$1" --nvram 2>/dev/null || true
  for d in sys1 sys2 hdd1 hdd2 hdd3 hdd4; do virsh -q vol-delete --pool $pool "$1-$d.qcow2" 2>/dev/null || true; done
}

shot() { # name out.png
  virsh -q screenshot "$1" "$2.ppm" >/dev/null
  python3 -c 'import sys; from PIL import Image; Image.open(sys.argv[1]).save(sys.argv[2])' "$2.ppm" "$2" && rm -f "$2.ppm"
}

bridge() { # name port [cols rows]
  mkdir -p "$state/$1"
  nohup $py "$here/serialbridge.py" --port "$2" --dir "$state/$1" --cols "${3:-80}" --rows "${4:-24}" \
    > "$state/$1/bridge.log" 2>&1 &
  echo $! > "$state/$1/bridge.pid"
}

key() { # name keys...: words are key names, anything else is typed
  local n=$1; shift
  $py "$here/serialbridge.py" --send "$state/$n/in" "$@"
}

screen() { cat "$state/$1/screen.txt"; }

"$@"
