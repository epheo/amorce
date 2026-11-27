#!/bin/bash
# mok.sh VM PORT: Secure Boot on, and the distro's published image, whose DRBD
# modules CI signs with the project key (PAYLOAD=msi-mok.iso media.sh
# ghcr.io/epheo/microshift:latest). A site file answers the wizard; the
# install queues the image's key; the test answers MokManager over the
# serial console with the password Ready shows. The installed system must
# then trust that key, and load DRBD signed by it under lockdown.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
image=${IMAGE:-localhost/amorce:dev}
v="$here/vm.sh"
work="$STATE/$vm"
log="$work/serial.log"
addr=10.99.0.41
export PAYLOAD=msi-mok-payload.iso
screen() { "$v" screen "$vm"; }
wait_for() { # pattern seconds
  for _ in $(seq 1 "$2"); do grep -q -E "$1" "$work/screen.txt" 2>/dev/null && return 0; sleep 1; done
  echo "timeout waiting for: $1"; screen; return 1
}
step() { echo "== $*"; }
on() { ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q "root@$addr" "$@"; }

step "site media"
kill "$(cat "$work/bridge.pid" 2>/dev/null)" 2>/dev/null || true
mkdir -p "$work/site" && rm -f "$work/id" "$work/id.pub" "$work/site/site.iso"
ssh-keygen -q -t ed25519 -N '' -C mok-test -f "$work/id"
cat > "$work/site/site.yaml" <<EOF
site: mok
defaults:
  purpose: microshift
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-MOK-01}
    name: mok-01
    address: $addr/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-mok-site.iso "$work/site/site.iso"

step "fresh VM, Secure Boot on"
"$v" flatnet
"$v" destroy "$vm"
"$v" create "$vm" "$port" on --sysinfo smbios,system.serial=MSI-MOK-01 \
  --disk vol=default/msi-mok-site.iso,device=cdrom,bus=sata \
  --xml "./devices/interface/source/@network=msi-flat"
: > "$log" && rm -f "$work/screen.txt"
"$v" bridge "$vm" "$port" 100 30

step "unattended install queues the key"
wait_for "is installed|The install stopped" 1200
grep -q "is installed" "$work/screen.txt" || { screen; exit 1; }
if grep -q "turn off Secure Boot" "$work/screen.txt"; then echo "the signed image still asks for Secure Boot off"; exit 1; fi
pw=$(grep -o -E '[0-9]{4} [0-9]{4}' "$work/screen.txt" | head -1 | tr -d ' ')
echo "MOK password: $pw"
[ -n "$pw" ]

step "MokManager, over the serial console"
# The firmware draws wider than the bridge's screen, so read its log.
mark=0
wait_log() { # pattern seconds: in what the console printed since the last key
  for _ in $(seq 1 "$2"); do tail -c +"$mark" "$log" | tr -d '\r' | grep -a -q -E "$1" && return 0; sleep 1; done
  echo "timeout waiting for: $1"; return 1
}
press() { mark=$(stat -c %s "$log"); "$v" key "$vm" "$@"; }
mark=$(stat -c %s "$log")
wait_log "Press any key to perform MOK management" 300
press space
wait_log "Enroll MOK" 30
press down enter
wait_log "View key 0" 30
press down enter
wait_log "Enroll the key" 30
press down enter
wait_log "Password" 30
press "$pw" enter
wait_log "Reboot" 30
sleep 1
press enter

step "the installed system trusts the key and loads DRBD under lockdown"
for _ in $(seq 1 120); do on true 2>/dev/null && break; sleep 5; done
on 'set -e
  dmesg | grep -i -E "secure ?boot.*enabled"
  cat /sys/kernel/security/lockdown
  cn=$(openssl x509 -inform der -in /usr/lib/amorce/mok.der -noout -subject -nameopt multiline | sed -n "s/^ *commonName *= //p")
  echo "the image key: $cn"
  dmesg | grep -F "Loaded X.509 cert '"'"'$cn"
  for _ in $(seq 1 60); do lsmod | grep -q "^drbd " && break; sleep 5; done
  lsmod | grep "^drbd "
  echo "drbd signer: $(modinfo -F signer drbd)"
  test "$(modinfo -F signer drbd)" = "$cn"
  ! dmesg | grep -i -E "key was rejected|module verification failed"'
echo PASS
