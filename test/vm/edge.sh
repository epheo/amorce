#!/bin/bash
# edge.sh VM PORT: the cases a lab rarely has. The data disks sit behind a
# hardware RAID volume, and the network has no DHCP and no gateway. The
# wizard must say so, install anyway, and the system must serve its UI.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
vm=$1 port=$2
v="$here/vm.sh"
log="$STATE/$vm/serial.log"
screen() { "$v" screen "$vm"; }
wait_for() { # pattern seconds
  for _ in $(seq 1 "$2"); do grep -q -E "$1" "$STATE/$vm/screen.txt" 2>/dev/null && return 0; sleep 1; done
  echo "timeout waiting for: $1"; screen; return 1
}
step() { echo "== $*"; }

step "fresh VM: PERC volume, flat network"
kill "$(cat "$STATE/$vm/bridge.pid" 2>/dev/null)" 2>/dev/null || true
"$v" flatnet
"$v" destroy "$vm"
# disk[3] is the first SCSI disk.
"$v" create "$vm" "$port" off \
  --xml "./devices/disk[3]/vendor=DELL" --xml "./devices/disk[3]/product=PERC H740P Mini" \
  --xml "./devices/interface/source/@network=msi-flat"
mkdir -p "$STATE/$vm" && : > "$log" && rm -f "$STATE/$vm/screen.txt"
"$v" bridge "$vm" "$port" "${COLS:-100}" "${ROWS:-30}"

step "welcome"
wait_for "On this media" 240
"$v" key "$vm" enter
step "network: no DHCP"
wait_for "No DHCP server answered" 120
"$v" key "$vm" enter
wait_for "Interface" 10
"$v" key "$vm" enter enter 10.99.0.11/24 enter enter enter enter
step "network: no gateway"
wait_for "only 10.99.0.0/24 is in reach" 60
wait_for "Continue with these settings" 5
"$v" key "$vm" enter
step "purpose"
wait_for "What will this machine do" 30
"$v" key "$vm" enter
step "disks: RAID volume"
wait_for "of the PERC H740P Mini controller" 30
wait_for "Use this layout" 5
"$v" key "$vm" enter
step "name"
wait_for "What should this machine be called" 30
name=$(grep -o '> [a-z0-9-]*' "$STATE/$vm/screen.txt" | head -1 | cut -c3-)
"$v" key "$vm" enter
step "confirm $name"
wait_for "to confirm" 30
"$v" key "$vm" "$name"
wait_for "Press enter to start" 10
"$v" key "$vm" enter
step "installing"
wait_for "is installed|The install stopped" 900
grep -q "is installed" "$STATE/$vm/screen.txt" || { screen; exit 1; }
"$v" key "$vm" enter
step "installed system"
url=https://10.99.0.11:30080
for _ in $(seq 1 120); do
  code=$(curl -sk -m 10 -o /dev/null -w '%{http_code}' "$url/" || true)
  [ "$code" = 200 ] && break
  sleep 5
done
echo "$url answers $code"
[ "$code" = 200 ]
echo PASS
