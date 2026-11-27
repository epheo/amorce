#!/bin/bash
# virt.sh VM PORT: the Virtualization purpose (dotvirt's image/, media from
# media.sh). A site file installs it unattended on a network with no
# registry in reach; the machine must come up whole from embedded images,
# serve dotvirt's console over HTTPS on its own address, say so on its
# screen, and let the first admin sign in with a token.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
image=${IMAGE:-localhost/amorce:dev}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
addr=10.99.0.71
step() { echo "== $*"; }
on() { ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q "root@$addr" "$@"; }
until_on() { # what seconds command: until command succeeds on the machine
  local what=$1 secs=$2; shift 2
  for _ in $(seq 1 $((secs / 10))); do on "$@" >/dev/null 2>&1 && { echo "$what"; return 0; }; sleep 10; done
  echo "timeout: $what"; on "$@" || true; return 1
}

step "site media"
kill "$(cat "$work/bridge.pid" 2>/dev/null)" 2>/dev/null || true
mkdir -p "$work/site" && rm -f "$work/id" "$work/id.pub" "$work/site/site.iso"
ssh-keygen -q -t ed25519 -N '' -C virt-test -f "$work/id"
cat > "$work/site/site.yaml" <<EOF
site: virt
defaults:
  purpose: virtualization
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-VIRT-01}
    name: virt-01
    address: $addr/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-virt-site.iso "$work/site/site.iso"

step "unattended install, no registry in reach"
"$v" flatnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" off --sysinfo smbios,system.serial=MSI-VIRT-01 \
  --disk vol=default/msi-virt-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-flat"
mkdir -p "$work" && : > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" 100 30
for _ in $(seq 1 1500); do grep -q -E "is installed|The install stopped" "$work/screen.txt" 2>/dev/null && break; sleep 1; done
grep -q "is installed" "$work/screen.txt" || { "$v" screen "$vm"; exit 1; }

step "the stack comes up from embedded images"
for _ in $(seq 1 120); do on true 2>/dev/null && break; sleep 5; done
oc="oc --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig"
until_on "KubeVirt available" 1800 "$oc -n kubevirt wait kubevirt/kubevirt --for=condition=Available --timeout=5s"
until_on "CDI available" 900 "$oc wait cdi/cdi --for=condition=Available --timeout=5s"
until_on "dotvirt available" 1800 \
  "$oc -n dotvirt get dotvirt dotvirt -o jsonpath='{.status.conditions[?(@.type==\"Available\")].status}' | grep -qx True"
until_on "the default Pool on the data disks" 300 "$oc get pools -o jsonpath='{.items[*].spec.vg}' | grep -qw myvg1"

step "the console, on the machine's own address"
for _ in $(seq 1 90); do
  code=$(curl -sk -m 10 -o /dev/null -w '%{http_code}' "https://$addr:30443/api/healthz" || true)
  [ "$code" = 200 ] && break
  sleep 10
done
echo "https://$addr:30443/api/healthz answers $code"
[ "$code" = 200 ]
served=$(openssl s_client -connect "$addr:30443" </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256 | cut -d= -f2)
on cat /run/issue.d/50-dotvirt.issue
on cat /run/issue.d/50-dotvirt.issue | grep -qF "TLS certificate SHA-256: $served"
on 'fold -w "$(stty -F /dev/tty1 size | cut -d" " -f2)" /dev/vcs1' | grep -q "dotvirt web console: https://$addr:30443"

step "the first admin signs in with a token"
tok=$(on "$oc create token dotvirt-admin -n dotvirt")
code=$(curl -sk -m 20 -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $tok" "https://$addr:30443/api/inventory")
echo "inventory with the token: $code"
[ "$code" = 200 ]
code=$(curl -sk -m 20 -o /dev/null -w '%{http_code}' "https://$addr:30443/api/inventory")
echo "inventory without one: $code"
[ "$code" = 401 ]
echo PASS
