#!/bin/bash
# join.sh PREFIX PORT: one cluster of three machines. The site file installs
# the first machine and a worker unattended, side by side; the worker joins
# once the first machine's MicroShift is up. A third machine, without the
# site file, hears the first machine on the network and joins it with the
# words on its screen. Needs media.sh first; on a network with no DHCP.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
pre=$1 port=$2
image=${IMAGE:-localhost/microshift-installer:dev}
v="$here/vm.sh"
work="$STATE/$pre"
step() { echo "== $*"; }
wait_screen() { # vm pattern seconds
  for _ in $(seq 1 "$3"); do grep -q -E "$2" "$STATE/$1/screen.txt" 2>/dev/null && return 0; sleep 1; done
  echo "timeout waiting for: $2"; "$v" screen "$1"; return 1
}
on() { # address command...
  local a=$1; shift
  ssh -i "$work/id" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -q "root@$a" "$@"
}
board() { on 10.99.0.21 installer machines 2>/dev/null || true; }
wait_board() { # jq filter seconds
  for _ in $(seq 1 "$2"); do board | jq -e "$1" >/dev/null 2>&1 && return 0; sleep 5; done
  echo "timeout waiting for board: $1"; board; return 1
}
kc() { on 10.99.0.21 kubectl --kubeconfig /var/lib/microshift/resources/kubeadmin/kubeconfig "$@"; }
ready_nodes() { kc get nodes --no-headers 2>/dev/null | awk '$2 == "Ready" {print $1}' | sort | tr '\n' ' '; }
boot() { # vm port serial [site]
  kill "$(cat "$STATE/$1/bridge.pid" 2>/dev/null)" 2>/dev/null || true
  "$v" destroy "$1"
  local media=()
  [ "${4:-}" = site ] && media=(--disk vol=default/msi-join-site.iso,device=cdrom,bus=sata)
  "$v" create "$1" "$2" off --sysinfo "smbios,system.serial=$3" "${media[@]}" \
    --xml "./devices/interface/source/@network=msi-flat"
  mkdir -p "$STATE/$1" && : > "$STATE/$1/serial.log" && rm -f "$STATE/$1/screen.txt"
  "$v" bridge "$1" "$2" 100 30
}

step "site media: a first machine and a worker"
mkdir -p "$work/site" && rm -f "$work/site/site.iso" "$work/id" "$work/id.pub"
ssh-keygen -q -t ed25519 -N '' -C join-test -f "$work/id"
token=$(head -c 24 /dev/urandom | base64 | tr -d '/+=')
cat > "$work/site/site.yaml" <<EOF
site: join
report: {address: 10.99.0.21, token: $token}
defaults:
  purpose: storage
  disks: {data: auto}
  profile: worker
  ssh_authorized_keys: ["$(cat "$work/id.pub")"]
machines:
  - match: {serial: MSI-JOIN-01}
    name: store-01
    address: 10.99.0.21/24
    profile: controller
  - match: {serial: MSI-JOIN-02}
    name: store-02
    address: 10.99.0.22/24
EOF
podman run --rm -v "$work/site:/site:Z" "$image" iso --payload-only --site /site/site.yaml --out /site/site.iso >/dev/null
"$v" upload msi-join-site.iso "$work/site/site.iso"
"$v" flatnet

step "first machine and worker install side by side"
boot "$pre-1" "$port" MSI-JOIN-01 site
boot "$pre-2" "$((port + 1))" MSI-JOIN-02 site
wait_screen "$pre-1" "is installed|The install stopped" 1200
wait_screen "$pre-2" "is installed|The install stopped" 1200
grep -q "is installed" "$STATE/$pre-1/screen.txt" && grep -q "is installed" "$STATE/$pre-2/screen.txt"

step "the worker joins"
wait_board '.[] | select(.name == "store-02" and .state == "joining")' 120
wait_board '.[] | select(.name == "store-02" and .state == "running" and .role == "worker")' 1800
for _ in $(seq 1 120); do [ "$(ready_nodes)" = "store-01 store-02 " ] && break; sleep 5; done
echo "ready: $(ready_nodes)"
[ "$(ready_nodes)" = "store-01 store-02 " ]
on 10.99.0.22 'test "$(microshift-profile)" = worker && ! ls /run/microshift-join-* /var/lib/microshift/bootstrap/kubeconfig 2>/dev/null &&
  ! systemctl is-failed -q greenboot-healthcheck'
on 10.99.0.21 'grep -q "^10.99.0.22 store-02 # microshift-installer worker$" /etc/hosts'

step "a third machine joins with the words on the first machine's screen"
boot "$pre-3" "$((port + 2))" MSI-JOIN-03
wait_screen "$pre-3" "On this media" 240
"$v" key "$pre-3" enter
wait_screen "$pre-3" "No DHCP server answered" 120
"$v" key "$pre-3" enter
wait_screen "$pre-3" "Interface" 10
"$v" key "$pre-3" enter enter 10.99.0.23/24 enter enter enter
wait_screen "$pre-3" "Continue with these settings" 60
"$v" key "$pre-3" enter
wait_screen "$pre-3" "Join store-01" 60
# The words are only on its screen: press F4 there, read them off tty1.
virsh -c qemu:///system send-key "$pre-1" KEY_F4 >/dev/null
tty1() { on 10.99.0.21 'fold -w "$(stty -F /dev/tty1 size | cut -d" " -f2)" /dev/vcs1'; }
words=
for _ in $(seq 1 10); do
  sleep 2
  words=$(tty1 | grep -A1 'and type:' | tail -1 | xargs || true)
  [ "$(echo "$words" | wc -w)" = 6 ] && break
done
echo "words: $words"
[ "$(echo "$words" | wc -w)" = 6 ] || { echo "no words on store-01's screen:"; tty1 | grep -v '^ *$'; exit 1; }
"$v" key "$pre-3" down enter
wait_screen "$pre-3" "The six words on its screen" 10
# Three letters of each word are enough.
"$v" key "$pre-3" "$(for w in $words; do printf '%s ' "${w:0:3}"; done)" enter
wait_screen "$pre-3" "Use this layout" 60
wait_board '.[] | select(.name == "store-03")' 30
"$v" key "$pre-3" enter
wait_screen "$pre-3" "What should this machine be called" 30
name=$(grep -o '> [a-z0-9-]*' "$STATE/$pre-3/screen.txt" | head -1 | cut -c3-)
echo "suggested name: $name"
[ "$name" = store-03 ]
"$v" key "$pre-3" enter
wait_screen "$pre-3" "to confirm" 30
"$v" key "$pre-3" "$name"
wait_screen "$pre-3" "Press enter to start" 10
"$v" key "$pre-3" enter
wait_screen "$pre-3" "is installed|The install stopped" 1200
grep -q "is installed" "$STATE/$pre-3/screen.txt"
"$v" key "$pre-3" enter
wait_board '.[] | select(.name == "store-03" and .state == "running" and .role == "worker")' 1800
for _ in $(seq 1 120); do [ "$(ready_nodes)" = "store-01 store-02 store-03 " ] && break; sleep 5; done
echo "ready: $(ready_nodes)"
[ "$(ready_nodes)" = "store-01 store-02 store-03 " ]
on 10.99.0.23 'grep -q "profile: worker" /etc/microshift-installer/site.yaml && test "$(stat -c %a /etc/microshift-installer/site.yaml)" = 600'
board | jq -r '.[] | "\(.name) \(.state) \(.address // "") \(.role // "")"'
"$v" shot "$pre-1" "$work/board.png"
echo "screen: $work/board.png"
echo PASS
