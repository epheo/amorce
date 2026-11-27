# Sourced by the dotvirt appliance's commands.

# Code points, not bytes, for the display name's length.
export LC_ALL=C.UTF-8
export KUBECONFIG=/var/lib/microshift/resources/kubeadmin/kubeconfig

# Tests run under a scratch ROOT.
ROOT=${ROOT:-}
SETTINGS=$ROOT/etc/dotvirt/appliance.env
STATE=$ROOT/var/lib/dotvirt-appliance
SHARE=$ROOT/usr/share/dotvirt-appliance
UNITS=$ROOT/etc/microshift/manifests.d
ISSUES=$ROOT/run/issue.d
# The installer's DNS check asks for these, so the domain fits all of them.
NAMES=(console git idm)

fail() {
    echo "$*" >&2
    exit 1
}

label_ok() {
    [[ $1 =~ ^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$ ]]
}

# The installer's rules (README, Names and sign-in): an image that refuses
# what the installer wrote would leave a machine installed and dead.
dns_name_ok() {
    local IFS=. labels l
    read -ra labels <<<"$1"
    [[ $1 != *. && ${#labels[@]} -ge 2 ]] || return 1
    for l in "${labels[@]}"; do label_ok "$l" || return 1; done
    [[ ${labels[-1]} =~ [^0-9] ]]
}

check_domain() {
    local n
    dns_name_ok "$1" || return 1
    for n in "${NAMES[@]}"; do
        ((${#n} + 1 + ${#1} <= 253)) || return 1
    done
}

check_email() {
    local local_part=${1%%@*} domain=${1#*@}
    [[ $1 == *@* && -n $local_part && $domain != *@* ]] || return 1
    ((${#1} <= 254 && ${#local_part} <= 64)) || return 1
    local bad='[^!#%&*+./0-9=?A-Z^_a-z{|}~-]'
    [[ ! $local_part =~ $bad ]] || return 1
    dns_name_ok "${domain,,}"
}

check_display() {
    local bad=$'[[:cntrl:]"\'\\$`]'
    ((${#1} <= 64)) && [[ ! $1 =~ $bad ]]
}

# read_settings sets DOMAIN, IDENTITY and ADMIN_*, or fails saying why. The
# file is parsed, never sourced: its values reach the CLI of Kanidm, never a
# shell.
read_settings() {
    local file=$1 line key value
    local -A seen=()
    DOMAIN="" IDENTITY="" ADMIN_USER="" ADMIN_DISPLAY="" ADMIN_EMAIL=""
    [ -s "$file" ] || fail "$file is empty"
    iconv -f UTF-8 -t UTF-8 "$file" >/dev/null 2>&1 || fail "$file is not UTF-8"
    [ -z "$(tail -c 1 "$file")" ] || fail "$file: the last line has no LF"
    while IFS= read -r line; do
        [[ $line == *=* ]] || fail "$file: not KEY=value: $line"
        key=${line%%=*} value=${line#*=}
        [ -z "${seen[$key]:-}" ] || fail "$file: $key twice"
        seen[$key]=1
        case $key in
        DOMAIN) DOMAIN=$value ;;
        IDENTITY) IDENTITY=$value ;;
        ADMIN_USER) ADMIN_USER=$value ;;
        ADMIN_DISPLAY) ADMIN_DISPLAY=$value ;;
        ADMIN_EMAIL) ADMIN_EMAIL=$value ;;
        *) fail "$file: unknown key $key" ;;
        esac
    done <"$file"

    [ -n "$DOMAIN" ] || fail "$file: no DOMAIN: the appliance serves its names under one"
    check_domain "$DOMAIN" || fail "$file: DOMAIN $DOMAIN is not a domain"
    case $IDENTITY in
    none)
        [ -z "${seen[ADMIN_USER]:-}${seen[ADMIN_DISPLAY]:-}${seen[ADMIN_EMAIL]:-}" ] ||
            fail "$file: ADMIN_ lines are for IDENTITY=kanidm"
        ;;
    kanidm)
        [[ $ADMIN_USER =~ ^[a-z][a-z0-9]{1,31}$ ]] || fail "$file: ADMIN_USER $ADMIN_USER is not a name"
        grep -qxF "$ADMIN_USER" "$SHARE/reserved-users" && fail "$file: ADMIN_USER $ADMIN_USER is taken by the system"
        check_email "$ADMIN_EMAIL" || fail "$file: ADMIN_EMAIL $ADMIN_EMAIL is not an email"
        check_display "$ADMIN_DISPLAY" || fail "$file: ADMIN_DISPLAY is not a name"
        : "${ADMIN_DISPLAY:=$ADMIN_USER}"
        ;;
    *) fail "$file: IDENTITY is none or kanidm, not ${IDENTITY:-empty}" ;;
    esac
}

# The CA's SHA-256 as browsers show it: byte pairs.
ca_fingerprint() {
    openssl x509 -in "$STATE/ca.crt" -noout -fingerprint -sha256 | cut -d= -f2
}

node_ip() {
    microshift show-config -m effective |
        awk '/^node:/ {n = 1; next} /^[^ ]/ {n = 0} n && $1 == "nodeIP:" {gsub(/"/, "", $2); print $2; exit}'
}
