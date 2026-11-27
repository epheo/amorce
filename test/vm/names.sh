#!/bin/bash
# names.sh VM PORT: names and sign-in, unattended. A site file gives an image
# labeled as the dotvirt appliance is a domain, Kanidm and its first admin,
# and a time server; the test network's DNS points *.virt.test at the machine
# but idm.virt.test elsewhere. The installed system must hold the image's
# settings file, chrony on the site's server, and in its log what DNS said.
# Then F2 moves the machine: a LoadBalancer Service follows the new address
# though node.nodeIP is pinned. Needs PAYLOAD=msi-names-payload.iso media.sh
# localhost/microshift:names (names.Containerfile).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
image=${IMAGE:-localhost/amorce:dev}
py=${PY:-python3}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
old=10.99.3.71 new=10.99.3.72
export PAYLOAD=msi-names-payload.iso
step() { echo "== $*"; }
opts() { echo "-i $work/id -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q"; }
on() { local a=$1; shift; ssh $(opts) "root@$a" "$@"; }
console() { "$py" "$here/ptydrive.py" ssh $(opts) -tt -p 2222 "root@$1"; }
kc="kubectl --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig"
lb() { on "$1" "$kc get service lb-test -o jsonpath='{.status.loadBalancer.ingress[0].ip}'" 2>/dev/null; }
up() { # address: until the system settled
  for _ in $(seq 1 120); do on "$1" true 2>/dev/null && break; sleep 5; done
  for _ in $(seq 1 120); do
    case "$(on "$1" systemctl is-system-running 2>/dev/null)" in running|degraded) return 0 ;; esac
    sleep 5
  done
  return 1
}

step "site media"
kill "$(cat "$work/bridge.pid" 2>/dev/null)" 2>/dev/null || true
mkdir -p "$work/site" && rm -f "$work/id" "$work/id.pub" "$work/site/site.iso"
ssh-keygen -q -t ed25519 -N '' -C names-test -f "$work/id"
cat > "$work/site/site.yaml" <<YAML
site: names
defaults:
  purpose: microshift
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
  network: {dns: [10.99.3.1], ntp: [10.99.3.1]}
  domain: virt.test
  identity: kanidm
  admin: {user: alice, display: Alice Martin, email: alice@virt.test}
machines:
  - match: {serial: MSI-NAMES-01}
    name: names-01
    address: $old/24
YAML
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-names-site.iso "$work/site/site.iso"

step "unattended install"
"$v" namesnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" off --sysinfo smbios,system.serial=MSI-NAMES-01 \
  --disk vol=default/msi-names-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-names"
mkdir -p "$work" && : > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" 100 30
for _ in $(seq 1 1200); do grep -q -E "is installed|The install stopped" "$work/screen.txt" 2>/dev/null && break; sleep 1; done
grep -q "is installed" "$work/screen.txt" || { "$v" screen "$vm"; exit 1; }
up "$old"

step "the image's settings file, chrony on the site's server, what DNS said"
on "$old" 'set -e
  cat /etc/dotvirt/appliance.env
  printf "DOMAIN=virt.test\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_DISPLAY=Alice Martin\nADMIN_EMAIL=alice@virt.test\n" |
    cmp - /etc/dotvirt/appliance.env
  ls -Z /etc/dotvirt/appliance.env | grep -q ":etc_t:"
  grep -qx "sourcedir /etc/amorce/chrony" /etc/chrony.conf
  ! grep -q "^pool " /etc/chrony.conf
  chronyc -n sources | grep 10.99.3.1
  grep -E "dns [a-z]+\.virt\.test" /var/log/amorce.log
  grep -q "dns console.virt.test: \[10.99.3.71\]" /var/log/amorce.log
  grep -q "dns idm.virt.test: \[10.99.3.99\]" /var/log/amorce.log'

step "a LoadBalancer Service gets the node IP"
for _ in $(seq 1 120); do on "$old" "$kc get --raw /readyz" 2>/dev/null | grep -q ok && break; sleep 5; done
on "$old" "$kc create service loadbalancer lb-test --tcp=18443:18443"
for _ in $(seq 1 60); do [ "$(lb "$old")" = "$old" ] && break; sleep 5; done
echo "lb-test: $(lb "$old")"
[ "$(lb "$old")" = "$old" ]

step "F2 shows the names and moves the machine"
console "$old" <<KEYS
wait This machine 60
keys f2
wait Sign-in +Kanidm 10
keys enter
wait NTP 5
keys ctrl+u $new/24 enter
wait restarts to use it 5
keys enter
wait Restarting 30
KEYS

step "the Service follows the new address"
up "$new"
for _ in $(seq 1 120); do [ "$(lb "$new")" = "$new" ] && break; sleep 5; done
echo "lb-test: $(lb "$new")"
on "$new" "grep -q 'nodeIP: $new' /etc/microshift/config.d/10-installer.yaml"
[ "$(lb "$new")" = "$new" ]
echo PASS
