#!/bin/bash
# build.sh [TAG]: the dotvirt appliance, with the pins of versions.env; any of
# them set in the environment wins. local/ holds what the build takes from
# here rather than a registry: operator.yaml, and images built here, as
# archives with a line "<archive> <ref>" each in local/images.
set -euo pipefail
cd "$(dirname "$0")"
eval "$(sed -n 's/^\([A-Z_]*\)=\(.*\)$/: "${\1=\2}"/p' versions.env)"
mkdir -p local
if [ -n "${DOTVIRT_OPERATOR_YAML:-}" ]; then
    cp "$DOTVIRT_OPERATOR_YAML" local/operator.yaml
else
    [ -n "$DOTVIRT_RELEASE" ] || { echo "no DOTVIRT_RELEASE, and no DOTVIRT_OPERATOR_YAML" >&2; exit 1; }
    curl -fsSL --retry 3 -o local/operator.yaml.new \
        "https://github.com/epheo/dotvirt/releases/download/$DOTVIRT_RELEASE/operator.yaml"
    echo "$DOTVIRT_OPERATOR_YAML_SHA256  local/operator.yaml.new" | sha256sum -c --quiet -
    mv local/operator.yaml.new local/operator.yaml
fi
podman build -t "${1:-localhost/dotvirt-appliance:dev}" -f Containerfile \
    --build-arg BASE_IMAGE="$BASE_IMAGE" \
    --build-arg PORTAIL_IMAGE="$PORTAIL_IMAGE" \
    --build-arg KANIDM_SERVER_IMAGE="$KANIDM_SERVER_IMAGE" \
    --build-arg KANIDM_TOOLS_IMAGE="$KANIDM_TOOLS_IMAGE" \
    --build-arg OPERATOR_IMAGE="${OPERATOR_IMAGE:-}" \
    --build-arg DOTVIRT_IMAGE="${DOTVIRT_IMAGE:-}" .
