#!/bin/bash
# purpose.sh VM PORT: the MicroShift purpose. The media hold it beside
# Storage (media.sh with both images); a site file picks MicroShift with its
# data disks. At the first start the distro's own unit builds them into VG
# myvg1 with thin pool thin, the recipe its README gives for doing it by
# hand, and its default Pool follows.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
image=${IMAGE:-localhost/amorce:dev}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
addr=10.99.0.61
step() { echo "== $*"; }
on() { ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q "root@$addr" "$@"; }

step "site media"
kill "$(cat "$work/bridge.pid" 2>/dev/null)" 2>/dev/null || true
mkdir -p "$work/site" && rm -f "$work/id" "$work/id.pub" "$work/site/site.iso"
ssh-keygen -q -t ed25519 -N '' -C purpose-test -f "$work/id"
cat > "$work/site/site.yaml" <<EOF
site: purpose
defaults:
  purpose: microshift
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-PUR-01}
    name: ms-01
    address: $addr/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-pur-site.iso "$work/site/site.iso"

step "unattended install of MicroShift"
"$v" flatnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" off --sysinfo smbios,system.serial=MSI-PUR-01 \
  --disk vol=default/msi-pur-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-flat"
mkdir -p "$work" && : > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" 100 30
for _ in $(seq 1 1200); do grep -q -E "is installed|The install stopped" "$work/screen.txt" 2>/dev/null && break; sleep 1; done
grep -q "is installed" "$work/screen.txt" || { "$v" screen "$vm"; exit 1; }

step "the distro builds the data disks into the default Pool"
for _ in $(seq 1 120); do on true 2>/dev/null && break; sleep 5; done
kc="kubectl --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig"
for _ in $(seq 1 120); do
  on "$kc get pools -o jsonpath='{.items[*].spec.vg}'" 2>/dev/null | grep -qw myvg1 && break
  sleep 5
done
on "set -e
  jq -c '{layout, pools: [.pools[] | {name, tier, n: (.devices | length)}]}' /etc/amorce/data-disks.json
  test \"\$(jq -r .layout /etc/amorce/data-disks.json)\" = thin
  systemctl is-active -q microshift-r-drbd-data-disks
  vgs --noheadings -o vg_name,pv_count,vg_size myvg1
  test \"\$(vgs --noheadings -o pv_count myvg1 | tr -d ' ')\" = 5
  lvs --noheadings -o lv_name,segtype myvg1/thin | grep -q thin-pool
  $kc get pools -o wide
  test \"\$($kc get storageclass r-drbd-local -o jsonpath='{.metadata.annotations.storageclass\\.kubernetes\\.io/is-default-class}')\" = true"
echo PASS
