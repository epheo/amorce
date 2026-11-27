# Run by identity in Kanidm's tools image as idm_admin, after a first line
# setting KANIDM_PASSWORD. Prints "<client> <secret>" for each client, and
# "link"/"expires" lines for an invite; everything else goes to the journal.
set -eu
exec 3>&1 1>&2
kanidm login
# get prints "No matching ..." and exits 0 when the entry is absent.
missing() { "$@" | grep -q '^No matching'; }

# Kanidm lets every person rename themselves by default; a name is an
# identity here, in the apiserver and the forge. Kanidm adds this member once,
# when the domain is created, so removing it holds across upgrades.
kanidm group remove-members idm_people_self_name_write idm_all_persons

# Its members are cluster-admin: only idm_admins may change them. Once in
# idm_high_privilege, its entry manager is no longer idm_admin's to set.
missing kanidm group get dotvirt_admins && kanidm group create dotvirt_admins idm_admins

# client NAME TITLE ORIGIN REDIRECT: confidential, so it may skip the
# consent screen. Every person may sign in; RBAC and the forge decide what
# they see.
client() {
    missing kanidm system oauth2 get "$1" && kanidm system oauth2 create "$1" "$2" "$3"
    kanidm system oauth2 add-redirect-url "$1" "$4"
    kanidm system oauth2 update-scope-map "$1" idm_all_persons openid profile email groups_name
    kanidm system oauth2 prefer-short-username "$1"
    kanidm system oauth2 disable-consent-prompt "$1"
}
client dotvirt "dotvirt console" "https://console.$DOMAIN/" "https://console.$DOMAIN/api/auth/callback"
# SSO: the forge's default name for its issuer, in its callback path.
client git "Git forge" "https://git.$DOMAIN/" "https://git.$DOMAIN/user/oauth2/SSO/callback"

# Only at the first run: later, Kanidm's admins own the first admin too. A
# display name or an email may start with -, which -- and --mail= keep from
# reading as a flag.
if [ -n "$FIRST" ]; then
    missing kanidm person get "$ADMIN_USER" && kanidm person create -- "$ADMIN_USER" "$ADMIN_DISPLAY"
    kanidm person update "$ADMIN_USER" --mail="$ADMIN_EMAIL"
    kanidm group add-members dotvirt_admins "$ADMIN_USER"
fi
if [ -n "$INVITE" ]; then
    token=$(kanidm person credential create-reset-token "$ADMIN_USER")
    echo "link $(printf '%s\n' "$token" | grep -o 'https://[^ ]*reset?token=[^ ]*' | head -1)" >&3
    echo "expires $(printf '%s\n' "$token" | sed -n 's/^This token will expire at: //p' | head -1)" >&3
fi

echo "dotvirt $(kanidm system oauth2 show-basic-secret dotvirt)" >&3
echo "git $(kanidm system oauth2 show-basic-secret git)" >&3
