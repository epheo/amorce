#!/bin/bash
# fleet.sh PREFIX PORT: one site file, two machines. The first installs and
# its console receives reports; the second reports its install to it, then
# that it runs. A third machine the file names never boots and stays listed.
# Needs msi-payload.iso built from an image with the console in it
# (test/vm/console.Containerfile); on a network with no DHCP and no gateway.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
pre=$1 port=$2
image=${IMAGE:-localhost/amorce:dev}
v="$here/vm.sh"
work="$STATE/$pre"
step() { echo "== $*"; }
wait_screen() { # vm pattern seconds
  for _ in $(seq 1 "$3"); do grep -q -E "$2" "$STATE/$1/screen.txt" 2>/dev/null && return 0; sleep 1; done
  echo "timeout waiting for: $2"; "$v" screen "$1"; return 1
}
token=$(head -c 12 /dev/urandom | base64 | tr -d '/+=')
first() { ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q root@10.99.0.21 "$@"; }
# The board answers only who proves the site's token, so ask the first machine.
board() { first amorce machines 2>/dev/null || true; }
wait_board() { # jq filter seconds
  for _ in $(seq 1 "$2"); do board | jq -e "$1" >/dev/null 2>&1 && return 0; sleep 2; done
  echo "timeout waiting for board: $1"; board; echo; return 1
}
boot() { # vm port serial
  kill "$(cat "$STATE/$1/bridge.pid" 2>/dev/null)" 2>/dev/null || true
  "$v" destroy "$1"
  "$v" create "$1" "$2" off --sysinfo "smbios,system.serial=$3" \
    --disk vol=default/msi-site.iso,device=cdrom,bus=sata \
    --xml "./devices/interface/source/@network=msi-flat"
  mkdir -p "$STATE/$1" && : > "$STATE/$1/serial.log" && rm -f "$STATE/$1/screen.txt"
  "$v" bridge "$1" "$2" 100 30
}

step "site media"
mkdir -p "$work/site" && rm -f "$work/site/site.iso" "$work/id" "$work/id.pub"
ssh-keygen -q -t ed25519 -N '' -C fleet-test -f "$work/id"
cat > "$work/site/site.yaml" <<EOF
site: fleet
report: {address: 10.99.0.21, token: $token}
defaults:
  purpose: storage
  disks: {data: auto}
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-FLEET-01}
    name: store-01
    address: 10.99.0.21/24
  - match: {serial: MSI-FLEET-02}
    name: store-02
    address: 10.99.0.22/24
  - match: {serial: MSI-FLEET-03}
    name: store-03
    address: 10.99.0.23/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-site.iso "$work/site/site.iso"
"$v" flatnet

step "first machine"
boot "$pre-1" "$port" MSI-FLEET-01
wait_screen "$pre-1" "is installed|The install stopped" 1200
grep -q "is installed" "$STATE/$pre-1/screen.txt" || exit 1
step "its console receives"
wait_board '.[] | select(.name == "store-01" and .state == "running")' 300
board | jq -e '[.[].name] | index("store-03")' >/dev/null

step "second machine reports to it"
boot "$pre-2" "$((port + 1))" MSI-FLEET-02
wait_board '.[] | select(.name == "store-02" and .state == "installing")' 600
echo "seen installing: $(board | jq -r '.[] | select(.name == "store-02") | "\(.step) \(.progress)"')"
wait_board '.[] | select(.name == "store-02" and .state == "installed")' 900
wait_board '.[] | select(.name == "store-02" and .state == "running" and .address == "10.99.0.22")' 600
board | jq -r '.[] | "\(.name) \(.state) \(.address // "")"'

step "the first machine's screen, kernel messages kept off it"
sleep 3
"$v" shot "$pre-1" "$work/board.png"
first 'systemctl is-active -q amorce-console &&
    for i in 1 2 3; do echo "<3>fleet test kernel message" > /dev/kmsg; done; sleep 1;
    ! grep -aq "fleet test kernel" /dev/vcs1 && grep -aq "fleet test kernel" /dev/vcs12' 
echo "screen: $work/board.png"
echo PASS
