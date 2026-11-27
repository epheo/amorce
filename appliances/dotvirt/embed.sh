#!/bin/bash
# In the image build: every image $1/images names into one OCI layout, and
# each archive $1/local/images names (built, not pulled), listed where the
# base's import-embedded-images reads them at every boot.
set -euo pipefail
LAYOUT=/usr/lib/embedded-images/dotvirt
MANIFEST=/usr/lib/embedded-images/manifest

while read -r ref; do
    [ -n "$ref" ] || continue
    key=$(tr '/:@' '___' <<<"$ref")
    echo "embedding $ref"
    skopeo copy -q --retry-times 5 --retry-delay 10s --preserve-digests \
        "docker://$ref" "oci:$LAYOUT:$key"
    echo "oci:$LAYOUT:$key $ref" >>"$MANIFEST"
done <"$1/images"

# Into the same layout, whatever podman save wrote: the boot import reads a
# lone .tar as an OCI archive only.
if [ -f "$1/local/images" ]; then
    while read -r archive ref; do
        [ -n "$ref" ] || continue
        format=docker-archive
        if tar -tf "$1/local/$archive" | grep -qx 'index.json'; then format=oci-archive; fi
        key=$(tr '/:@' '___' <<<"$ref")
        echo "embedding $ref from $archive ($format)"
        skopeo copy -q "$format:$1/local/$archive" "oci:$LAYOUT:$key"
        echo "oci:$LAYOUT:$key $ref" >>"$MANIFEST"
    done <"$1/local/images"
fi
echo "embedded: $(du -sh /usr/lib/embedded-images | cut -f1)"
