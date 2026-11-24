#!/bin/bash
# settings.sh VM PORT: the installed console's settings. A site file installs
# one machine; over SSH to its console (port 2222) the test sets a root
# password and a new address; the machine restarts on it, MicroShift and the
# UI follow, and the password logs in on the serial console. F2 must open
# the settings on tty1 too. Needs media.sh first.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
image=${IMAGE:-localhost/microshift-installer:dev}
py=${PY:-python3}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
old=10.99.0.51 new=10.99.0.52 pw=horse-battery-9
step() { echo "== $*"; }
opts() { echo "-i $work/id -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q"; }
on() { local a=$1; shift; ssh $(opts) "root@$a" "$@"; }
console() { "$py" "$here/ptydrive.py" ssh $(opts) -tt -p 2222 "root@$1"; }
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
ssh-keygen -q -t ed25519 -N '' -C settings-test -f "$work/id"
cat > "$work/site/site.yaml" <<EOF
site: settings
defaults:
  purpose: storage
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-SET-01}
    name: set-01
    address: $old/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-set-site.iso "$work/site/site.iso"

step "unattended install"
"$v" flatnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" off --sysinfo smbios,system.serial=MSI-SET-01 \
  --disk vol=default/msi-set-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-flat"
mkdir -p "$work" && : > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" 100 30
for _ in $(seq 1 1200); do grep -q -E "is installed|The install stopped" "$work/screen.txt" 2>/dev/null && break; sleep 1; done
grep -q "is installed" "$work/screen.txt"
up "$old"

step "over SSH to the console: a root password, then a new address"
console "$old" <<EOF
wait This machine 60
keys f2
wait Settings 10
keys down enter
wait Again 5
keys $pw enter $pw enter
wait Root password set 30
keys up enter
wait Gateway 5
keys ctrl+u $new/24 enter
wait restarts to use it 5
keys enter
wait Restarting 30
EOF

step "the machine is back on its new address"
up "$new"
for _ in $(seq 1 60); do
  on "$new" kubectl --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig get node set-01 \
    -o jsonpath='{.status.addresses[0].address} {.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null |
    grep -q "^$new True$" && break
  sleep 5
done
on "$new" 'set -e
  kubectl --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig get node set-01 --no-headers -o wide
  grep -q "nodeIP: '"$new"'" /etc/microshift/config.d/10-installer.yaml
  grep -q "address: '"$new"'/24" /etc/microshift-installer/site.yaml'
for _ in $(seq 1 120); do
  code=$(curl -sk -m 10 -o /dev/null -w '%{http_code}' "https://$new:30080/" || true)
  [ "$code" = 200 ] && break
  sleep 5
done
echo "https://$new:30080 answers $code"
[ "$code" = 200 ]

step "the root password logs in on the serial console"
"$v" key "$vm" enter
sleep 2
"$v" key "$vm" root enter
sleep 2
"$v" key "$vm" "$pw" enter
sleep 3
"$v" key "$vm" 'echo LOGGED-IN-$((6*7))' enter
sleep 2
tr -d '\r' < "$log" | grep -a -q LOGGED-IN-42

step "F2 on tty1"
virsh -c qemu:///system send-key "$vm" KEY_F2 >/dev/null
sleep 2
on "$new" 'fold -w "$(stty -F /dev/tty1 size | cut -d" " -f2)" /dev/vcs1' | grep -q "Root password   set"
echo PASS
