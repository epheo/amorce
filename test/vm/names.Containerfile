# An OS image labeled as the dotvirt appliance is, for names.sh: the
# installer's half of names and sign-in, on an image that ignores them.
#   podman build -t localhost/microshift:names -f test/vm/names.Containerfile test/vm
ARG BASE=ghcr.io/epheo/microshift:latest
FROM $BASE
LABEL io.github.epheo.installer.domain="console;git" \
      io.github.epheo.installer.identity="kanidm:idm" \
      io.github.epheo.installer.settings="/etc/dotvirt/appliance.env" \
      io.github.epheo.installer.reserved-users="admin;anonymous;api;assets;attachments;avatar;avatars;captcha;login;metrics;org;v2"
