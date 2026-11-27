#!/bin/bash
# setup.sh: the appliance's setup under a scratch root, for both sign-ins,
# and its settings checks. Needs bash, openssl and oc (kustomize).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0
check() { # what command...: the command must succeed
    local what=$1
    shift
    if "$@" >"$work/out" 2>&1; then echo "ok   $what"; else
        echo "FAIL $what"; sed 's/^/     /' "$work/out"; fails=$((fails + 1))
    fi
}
refused() { # what pattern settings: setup must refuse the settings, saying pattern
    local what=$1 pattern=$2
    root "$3"
    if ROOT=$ROOT bash "$here/rootfs/usr/libexec/dotvirt-appliance/setup" >"$work/out" 2>&1; then
        echo "FAIL $what: accepted"; fails=$((fails + 1))
    elif grep -q -- "$pattern" "$work/out"; then echo "ok   $what"; else
        echo "FAIL $what: $(cat "$work/out")"; fails=$((fails + 1))
    fi
}
root() { # settings: a fresh root holding them
    ROOT=$work/root.$RANDOM
    mkdir -p "$ROOT/etc/dotvirt" "$ROOT/usr/share/dotvirt-appliance"
    cp -r "$here/machine" "$ROOT/usr/share/dotvirt-appliance/"
    # What render.sh fills in at build time.
    sed -i -e 's|@PORTAIL_IMAGE@|localhost/embedded/portail:test|' -e 's|@KANIDM_SERVER_IMAGE@|docker.io/kanidm/server:test|' \
        "$ROOT"/usr/share/dotvirt-appliance/machine/*/*.yaml
    sed -n 's/^ARG RESERVED_USERS="\(.*\)"$/\1/p' "$here/Containerfile" | tr ';' '\n' \
        >"$ROOT/usr/share/dotvirt-appliance/reserved-users"
    printf '%s' "$1" >"$ROOT/etc/dotvirt/appliance.env"
}
setup() { ROOT=$ROOT bash "$here/rootfs/usr/libexec/dotvirt-appliance/setup" "$@"; }
# oc on PATH: kustomize is the real one's, apply records what reaches the cluster.
mkdir -p "$work/bin"
real_oc=$(command -v oc)
printf '%s\n' '#!/bin/bash' "[ \"\$1\" = apply ] || exec '$real_oc' \"\$@\"" \
    "grep -h '^kind:' >>'$work/applied'" >"$work/bin/oc"
chmod +x "$work/bin/oc"
export PATH=$work/bin:$PATH
# signs NAME SAN: the machine CA signs a leaf for SAN that verifies under it.
signs() {
    local d=$ROOT/var/lib/dotvirt-appliance
    openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$1" \
        -keyout "$work/$1.key" -out "$work/$1.csr" 2>/dev/null &&
        openssl x509 -req -in "$work/$1.csr" -CA "$d/ca.crt" -CAkey "$d/ca.key" -days 1 \
            -out "$work/$1.crt" -extfile <(echo "subjectAltName=$2") 2>/dev/null &&
        openssl verify -CAfile "$d/ca.crt" "$work/$1.crt"
}
unsigned() { ! signs "$@"; }
builds() { # unit: it builds, and names no placeholder
    oc kustomize "$ROOT/etc/microshift/manifests.d/$1" >"$work/$1.yaml" && ! grep -n '@[A-Z_]*@' "$work/$1.yaml"
}
has() { grep -q -- "$2" "$work/$1.yaml"; }

kanidm=$'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_DISPLAY=Alice Martin\nADMIN_EMAIL=alice@example.com\n'
root "$kanidm"
check "kanidm: setup runs" setup
check "kanidm: the edge builds" builds dotvirt-edge
check "kanidm: the identity builds" builds dotvirt-identity
check "kanidm: the install builds" builds dotvirt-install
check "kanidm: the console signs in through Kanidm" has dotvirt-install "issuerURL: https://idm.virt.example.com/oauth2/openid/dotvirt"
check "kanidm: Kanidm's admins are cluster-admin" has dotvirt-install "name: idm:dotvirt_admins"
check "kanidm: no token account" bash -c "! grep -q dotvirt-admin '$work/dotvirt-install.yaml'"
check "kanidm: the edge serves console.virt.example.com" has dotvirt-edge "hostname: console.virt.example.com"
check "kanidm: the apiserver trusts the CA" grep -q "BEGIN CERTIFICATE" "$ROOT/etc/microshift/dotvirt-appliance-authentication.yaml"
check "kanidm: the CA signs the edge" openssl verify -CAfile "$ROOT/var/lib/dotvirt-appliance/ca.crt" "$ROOT/var/lib/dotvirt-appliance/edge.crt"
check "kanidm: the CA signs git.virt.example.com" signs git DNS:git.virt.example.com
check "kanidm: the CA signs no other name under the domain" unsigned www DNS:www.virt.example.com
check "kanidm: the CA signs no IPv4 address" unsigned ip4 IP:10.0.0.1
check "kanidm: the CA signs no IPv6 address" unsigned ip6 IP:fd00::1
check "kanidm: the screen gives the console" grep -q "https://console.virt.example.com" "$ROOT/run/issue.d/50-dotvirt.issue"
ca=$(sha256sum "$ROOT/var/lib/dotvirt-appliance/ca.crt")
check "kanidm: a second start keeps the CA" setup
check "kanidm: same CA" bash -c "[ \"\$(sha256sum '$ROOT/var/lib/dotvirt-appliance/ca.crt')\" = '$ca' ]"
rm -f "$work/applied"
check "kanidm: renew with nothing near its end" setup renew
check "kanidm: renew still hands the cluster both pairs" bash -c "[ \$(grep -c '^kind: Secret' '$work/applied') = 2 ]"
sed -i 's/^DOMAIN=.*/DOMAIN=other.example.com/' "$ROOT/etc/dotvirt/appliance.env"
check "kanidm: a new domain is refused" bash -c "! ROOT='$ROOT' bash '$here/rootfs/usr/libexec/dotvirt-appliance/setup'"

root $'DOMAIN=virt.example.com\nIDENTITY=none\n'
check "none: setup runs" setup
check "none: the install builds" builds dotvirt-install
check "none: a token account" has dotvirt-install "name: dotvirt-admin"
check "none: no OIDC" bash -c "! grep -q oidc '$work/dotvirt-install.yaml'"
check "none: no Kanidm" test ! -e "$ROOT/etc/microshift/manifests.d/dotvirt-identity"
check "none: the apiserver unchanged" test ! -e "$ROOT/etc/microshift/config.d/40-dotvirt-appliance.yaml"

# The installer accepts these; Kanidm's CLI gets them past -- and --mail=.
root $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_DISPLAY=-Alice\nADMIN_EMAIL=-alice@example.com\n'
check "a leading dash in the name and the email" setup

refused "no DOMAIN" "no DOMAIN" $'IDENTITY=none\n'
refused "an empty DOMAIN" "no DOMAIN" $'DOMAIN=\nIDENTITY=none\n'
refused "an address" "not a domain" $'DOMAIN=10.0.0.1\nIDENTITY=none\n'
refused "an unknown key" "unknown key" $'DOMAIN=virt.example.com\nIDENTITY=none\nPORT=1\n'
refused "a key twice" "twice" $'DOMAIN=virt.example.com\nDOMAIN=virt.example.com\nIDENTITY=none\n'
refused "no final LF" "no LF" $'DOMAIN=virt.example.com\nIDENTITY=none'
refused "CRLF" "not a domain" $'DOMAIN=virt.example.com\r\nIDENTITY=none\n'
refused "an admin without Kanidm" "for IDENTITY=kanidm" $'DOMAIN=virt.example.com\nIDENTITY=none\nADMIN_USER=alice\n'
refused "a reserved admin" "taken" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=admin\nADMIN_EMAIL=a@example.com\n'
refused "a Kanidm client's name" "taken" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=git\nADMIN_EMAIL=a@example.com\n'
refused "a name Forgejo reserves" "taken" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=user\nADMIN_EMAIL=a@example.com\n'
refused "a name Kanidm refuses" "taken" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=root\nADMIN_EMAIL=a@example.com\n'
refused "a non-ASCII letter in the email's domain" "not an email" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_EMAIL=a@\u212aexample.com\n'
refused "a bad email" "not an email" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_EMAIL=alice\n'
refused "a quote in the name" "not a name" $'DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_DISPLAY=Al"ice\nADMIN_EMAIL=a@example.com\n'
refused "an unknown provider" "none or kanidm" $'DOMAIN=virt.example.com\nIDENTITY=keycloak\n'

[ "$fails" = 0 ] || { echo "$fails failed"; exit 1; }
echo "all passed"
