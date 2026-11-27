#!/bin/bash
# In the image build: manifests.d, completed with the upstream manifests
# manifests.sha256 pins and dotvirt's operator.yaml, into $OUT/manifests.d;
# the per-machine templates with their images filled in, into $OUT/share;
# every image they run, into $OUT/images.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
: "${OUT:?}" "${PORTAIL_IMAGE:?}" "${KANIDM_SERVER_IMAGE:?}" "${KANIDM_TOOLS_IMAGE:?}"
units=$OUT/manifests.d share=$OUT/share
mkdir -p "$units" "$share"
cp -r "$here/manifests.d/." "$units/"
cp -r "$here/machine" "$share/"

grep -v '^#' "$here/manifests.sha256" | while read -r sum file url; do
    [ -n "$file" ] || continue
    curl -fsSL --retry 3 -o "$units/$file" "$url"
    echo "$sum  $units/$file" | sha256sum -c --quiet -
done

# portail runs the image the base embeds, controller and data planes alike;
# a base embedding another release would leave the edge with no image.
grep -q " $PORTAIL_IMAGE\$" /usr/lib/embedded-images/manifest ||
    { echo "the base embeds no $PORTAIL_IMAGE: set PORTAIL_IMAGE to its portail" >&2; exit 1; }
sed -i "s|image: ghcr.io/epheo/portail$|image: $PORTAIL_IMAGE|" "$units/016-portail/controller.yaml"
grep -q "image: $PORTAIL_IMAGE$" "$units/016-portail/controller.yaml"

d=$units/050-dotvirt-operator
mkdir -p "$d"
cp "$here/local/operator.yaml" "$d/operator.yaml"
# Images built rather than released, for tests: their archives are in local/.
if [ -n "${OPERATOR_IMAGE:-}" ]; then
    sed -i -E "s#image: quay.io/epheo/dotvirt-operator@sha256:[0-9a-f]{64}#image: $OPERATOR_IMAGE#" "$d/operator.yaml"
    grep -q "image: $OPERATOR_IMAGE$" "$d/operator.yaml"
fi
if [ -n "${DOTVIRT_IMAGE:-}" ]; then
    sed -i -E "/name: RELATED_IMAGE_DOTVIRT$/{n;s#value: .*#value: $DOTVIRT_IMAGE#}" "$d/operator.yaml"
    grep -q "value: $DOTVIRT_IMAGE$" "$d/operator.yaml"
fi
# Forgejo publishes the same digests on code.forgejo.org as on codeberg.org,
# whose registry refuses some builders.
sed -i "s#codeberg.org/forgejo/forgejo:#code.forgejo.org/forgejo/forgejo:#" "$d/operator.yaml"
printf '%s\n' "apiVersion: kustomize.config.k8s.io/v1beta1" "kind: Kustomization" "resources:" "  - operator.yaml" \
    >"$d/kustomization.yaml"

for f in "$share"/machine/*/*.yaml; do
    sed -i -e "s|@PORTAIL_IMAGE@|$PORTAIL_IMAGE|g" -e "s|@KANIDM_SERVER_IMAGE@|$KANIDM_SERVER_IMAGE|g" "$f"
done
printf '%s\n' "$KANIDM_TOOLS_IMAGE" >"$share/kanidm-tools-image"

# An embedded image keeps its manifest byte for byte, and the boot import
# refuses a digest that does not match it: an index digest becomes the
# amd64 instance's.
grep -rhoE '[a-z0-9.-]+\.[a-z]+/[a-z0-9._/-]+(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}' "$units" "$share" | sort -u |
    while read -r ref; do
        raw=$(skopeo inspect --raw "docker://$(sed -E 's/:[A-Za-z0-9._-]+@/@/' <<<"$ref")")
        inst=$(jq -r 'if .manifests then [.manifests[] | select(.platform.architecture=="amd64" and .platform.os=="linux")][0].digest else empty end' <<<"$raw")
        [ -n "$inst" ] || continue
        grep -rlF "${ref##*@}" "$units" "$share" | xargs sed -i "s|${ref##*@}|$inst|g"
    done
# podman and the kubelet find an embedded image by digest alone.
sed -i -E 's/:[A-Za-z0-9._-]+@sha256:/@sha256:/' "$share/kanidm-tools-image"
# Always would pull past the embedded copy at every pod start.
grep -rl 'imagePullPolicy: Always' "$units" | xargs -r sed -i 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/'

# Every image the units and templates run, and the operands only KubeVirt's
# operator knows (operands), which must be of the KubeVirt the units install.
kubevirt=$(grep -ohE 'quay.io/kubevirt/virt-operator:v[0-9.]+' "$units/030-kubevirt/kubevirt-operator.yaml" | head -1)
grep -q "^quay.io/kubevirt/virt-api:${kubevirt##*:}$" "$here/operands" ||
    { echo "operands is not of KubeVirt ${kubevirt##*:}" >&2; exit 1; }
{
    grep -rhoE '(image|value): *"?[a-z0-9.-]+\.[a-z]+/[a-z0-9._/-]+(:[A-Za-z0-9._-]+)?(@sha256:[0-9a-f]{64})?' "$units" "$share" |
        sed -E 's/^(image|value): *"?//'
    cat "$share/kanidm-tools-image"
    grep -v '^#' "$here/operands"
} | sed -E 's/:[A-Za-z0-9._-]+@sha256:/@sha256:/' | grep -v '^localhost/' | grep . | sort -u >"$OUT/images"
echo "units: $(ls "$units" | tr '\n' ' ')"
echo "images: $(wc -l <"$OUT/images")"
