#!/bin/bash
# pools.sh VM: lists the StoragePools the installed stornas reports, logging
# in with the first admin password its console prints.
set -euo pipefail
log="$STATE/$1/serial.log"
url=$(tr -d '\r' < "$log" | grep -a -o 'https://[0-9.]*:30080' | tail -1)
pw=$(tr -d '\r' < "$log" | grep -a -o 'Initial password for admin: [A-Z0-9]*' | tail -1 | awk '{print $NF}')
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT
curl -sk -m 10 -c "$jar" -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$pw\"}" "$url/api/v1/login" >/dev/null
curl -sk -m 20 -b "$jar" "$url/api/v1/state" | python3 -c '
import json, sys
for p in json.load(sys.stdin).get("pools", []):
    print(p["name"], p["node"], p["raid"], p["tier"], len(p["devices"]), "disks", p.get("health", ""))
'
