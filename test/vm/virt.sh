#!/bin/bash
# virt.sh VM PORT [none|kanidm]: the dotvirt appliance, from
# PAYLOAD=msi-virt-payload.iso media.sh localhost/dotvirt-appliance:dev.
# A site file installs it unattended with domain virt.test, on a network
# whose DNS points *.virt.test at the machine and with no registry in reach.
# The machine must come up whole from embedded images, and serve the console
# and the forge by name under the CA its screen shows.
# none: a token from the machine signs the first admin in.
# kanidm: Kanidm answers on idm.virt.test, the screen holds the first admin's
# invite, and the console sends sign-in to Kanidm.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2 id=${3:-none}
image=${IMAGE:-localhost/amorce:dev}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
addr=10.99.4.71 domain=virt.test
export PAYLOAD=msi-virt-payload.iso
step() { echo "== $*"; }
on() { ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q "root@$addr" "$@"; }
until_on() { # what seconds command: until command succeeds on the machine
  local what=$1 secs=$2; shift 2
  for _ in $(seq 1 $((secs / 10))); do on "$@" >/dev/null 2>&1 && { echo "$what"; return 0; }; sleep 10; done
  echo "timeout: $what"; on "$@" || true; return 1
}
web() { # name path [curl args]: the answer's code, by name, through the machine's CA
  local n=$1.$domain p=$2; shift 2
  curl -s -m 20 -o /dev/null -w '%{http_code}' --cacert "$work/ca.pem" --resolve "$n:443:$addr" "$@" "https://$n$p" || true
}
until_web() { # what name path code
  local c
  for _ in $(seq 1 90); do c=$(web "$2" "$3"); [ "$c" = "$4" ] && { echo "$1: $c"; return 0; }; sleep 10; done
  echo "timeout: $1: $c"; return 1
}
screen() { on 'fold -w "$(stty -F /dev/tty1 size | cut -d" " -f2)" /dev/vcs1'; }

case $id in
none) admin= ;;
kanidm) admin="admin: {user: alice, display: Alice Martin, email: alice@$domain}" ;;
*) echo "sign-in is none or kanidm"; exit 2 ;;
esac

step "site media, sign-in $id"
kill "$(cat "$work/bridge.pid" 2>/dev/null)" 2>/dev/null || true
mkdir -p "$work/site" && rm -f "$work/id" "$work/id.pub" "$work/site/site.iso" "$work/ca.pem"
ssh-keygen -q -t ed25519 -N '' -C virt-test -f "$work/id"
cat > "$work/site/site.yaml" <<YAML
site: virt
defaults:
  purpose: virtualization
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
  network: {dns: [10.99.4.1]}
  domain: $domain
  identity: $id
  $admin
machines:
  - match: {serial: MSI-VIRT-01}
    name: virt-01
    address: $addr/24
YAML
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-virt-site.iso "$work/site/site.iso"

step "unattended install, no registry in reach"
"$v" virtnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" off --sysinfo smbios,system.serial=MSI-VIRT-01 \
  --disk vol=default/msi-virt-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-virt"
mkdir -p "$work" && : > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" 100 30
for _ in $(seq 1 1500); do grep -q -E "is installed|The install stopped" "$work/screen.txt" 2>/dev/null && break; sleep 1; done
grep -q "is installed" "$work/screen.txt" || { "$v" screen "$vm"; exit 1; }

step "the settings file, and what DNS said"
for _ in $(seq 1 120); do on true 2>/dev/null && break; sleep 5; done
on cat /etc/dotvirt/appliance.env
on grep -q "^DOMAIN=$domain$" /etc/dotvirt/appliance.env
on grep -q "^IDENTITY=$id$" /etc/dotvirt/appliance.env
for n in console git; do on "grep -qF 'dns $n.$domain: [$addr]' /var/log/amorce.log"; done

step "the stack comes up from embedded images"
oc="oc --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig"
until_on "KubeVirt available" 1800 "$oc -n kubevirt wait kubevirt/kubevirt --for=condition=Available --timeout=5s"
until_on "CDI available" 900 "$oc wait cdi/cdi --for=condition=Available --timeout=5s"
until_on "dotvirt available" 1800 \
  "$oc -n dotvirt-operator get dotvirt -o jsonpath='{.items[0].status.conditions[?(@.type==\"Available\")].status}' | grep -qx True"
until_on "the default Pool on the data disks" 300 "$oc get pools -o jsonpath='{.items[*].spec.vg}' | grep -qw myvg1"
until_on "r-drbd-local the default class" 120 \
  "$oc get sc r-drbd-local -o jsonpath='{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}' | grep -qx true"

step "the console and the forge, by name, under the machine's CA"
on dotvirt-appliance ca > "$work/ca.pem"
fp=$(openssl x509 -in "$work/ca.pem" -noout -fingerprint -sha256 | cut -d= -f2)
echo "CA SHA-256: $fp"
until_web "console https://console.$domain/api/healthz" console /api/healthz 200
until_web "forge https://git.$domain/user/login" git /user/login 200
on cat /run/issue.d/50-dotvirt.issue
# Compared as hex, whatever the screen's line breaks.
on cat /run/issue.d/50-dotvirt.issue | tr -cd '0-9A-Fa-f' | tr a-f A-F | grep -qF "${fp//:/}"
screen | grep -q "https://console.$domain"

if [ "$id" = none ]; then
  step "the first admin signs in with a token"
  tok=$(on dotvirt-appliance token)
  c=$(web console /api/inventory -H "Authorization: Bearer $tok")
  echo "inventory with the token: $c"
  [ "$c" = 200 ]
else
  step "Kanidm, the first admin's invite, and sign-in through Kanidm"
  until_web "Kanidm https://idm.$domain/status" idm /status 200
  on cat /run/issue.d/51-dotvirt-invite.issue
  on grep -q "https://idm.$domain/" /run/issue.d/51-dotvirt-invite.issue
  screen | grep -q "idm.$domain"
  # A console that cannot reach the issuer sends the browser to /?sso_error=1.
  to=$(curl -s -m 20 -o /dev/null -w '%{http_code} %{redirect_url}' --cacert "$work/ca.pem" \
    --resolve "console.$domain:443:$addr" "https://console.$domain/api/auth/oidc" || true)
  echo "sign-in starts at: $to"
  case $to in "302 https://idm.$domain/"*) ;; *) exit 1 ;; esac
fi
c=$(web console /api/inventory)
echo "inventory without a token: $c"
[ "$c" = 401 ]
echo PASS
