#!/bin/bash
# site.sh VM PORT: a site file on its own medium answers every screen; no
# key is pressed until the install is done. The VM has a serial number the
# file names, and the network has no DHCP and no gateway. The installed
# system must serve its UI, take the file's SSH key, and keep its answers.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
image=${IMAGE:-localhost/amorce:dev}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
screen() { "$v" screen "$vm"; }
wait_for() { # pattern seconds
  for _ in $(seq 1 "$2"); do grep -q -E "$1" "$work/screen.txt" 2>/dev/null && return 0; sleep 1; done
  echo "timeout waiting for: $1"; screen; return 1
}
step() { echo "== $*"; }

step "site media"
kill "$(cat "$work/bridge.pid" 2>/dev/null)" 2>/dev/null || true
mkdir -p "$work/site" && rm -f "$work/id" "$work/id.pub" "$work/site/site.iso"
ssh-keygen -q -t ed25519 -N '' -C site-test -f "$work/id"
cat > "$work/site/site.yaml" <<EOF
site: vmlab
defaults:
  purpose: storage
  keyboard: fr
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-SITE-01}
    name: store-02
    address: 10.99.0.12/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-site.iso "$work/site/site.iso"

step "fresh VM: serial MSI-SITE-01, flat network"
"$v" flatnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" off \
  --sysinfo smbios,system.serial=MSI-SITE-01 \
  --disk vol=default/msi-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-flat"
: > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" "${COLS:-100}" "${ROWS:-30}"

step "site run"
wait_for "This is store-02" 240
wait_for "Installing in [0-9]+ s" 120
step "installing, untouched"
wait_for "is installed|The install stopped" 900
grep -q "is installed" "$work/screen.txt" || { screen; exit 1; }

step "installed system"
url=https://10.99.0.12:30080
for _ in $(seq 1 120); do
  code=$(curl -sk -m 10 -o /dev/null -w '%{http_code}' "$url/" || true)
  [ "$code" = 200 ] && break
  sleep 5
done
echo "$url answers $code"
[ "$code" = 200 ]

step "SSH key and kept answers"
ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 \
  root@10.99.0.12 cat /etc/amorce/site.yaml > "$work/kept.yaml"
grep -q "name: store-02" "$work/kept.yaml"
grep -q "serial: MSI-SITE-01" "$work/kept.yaml"
grep -q "site-test" "$work/kept.yaml"
echo PASS
