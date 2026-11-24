#!/bin/bash
# e2e.sh VM PORT: installs the Storage purpose through the wizard on a fresh
# VM, over its serial console, then checks the installed system serves its UI.
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

step "fresh VM"
kill "$(cat "$STATE/$vm/bridge.pid" 2>/dev/null)" 2>/dev/null || true
"$v" destroy "$vm"
"$v" create "$vm" "$port"
mkdir -p "$STATE/$vm" && : > "$log" && rm -f "$STATE/$vm/screen.txt"
"$v" bridge "$vm" "$port" "${COLS:-100}" "${ROWS:-30}"

step "welcome"
wait_for "Install on this machine" 180
wait_for "On this media" 60
"$v" key "$vm" enter
step "network"
wait_for "Keep these settings|What should we do" 90
wait_for "Keep .*as a fixed address|Continue with these settings" 60
"$v" key "$vm" enter
step "purpose"
wait_for "What will this machine do" 30
"$v" key "$vm" enter
step "disks"
wait_for "Use this layout" 30
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
step "installed system"
"$v" key "$vm" enter
for _ in $(seq 1 600); do
  url=$(tr -d '\r' < "$log" | grep -a -o 'https://[0-9.]*:30080' | tail -1 || true)
  [ -n "$url" ] && break
  sleep 1
done
[ -n "$url" ] || { echo "no web address on the console"; exit 1; }
code=$(curl -sk -m 10 -o /dev/null -w '%{http_code}' "$url/")
echo "$url answers $code"
[ "$code" = 200 ]
tr -d '\r' < "$log" | grep -a -q "^$name login:" && echo "hostname $name"
echo PASS
