# An OS image whose DRBD modules are signed the way the distro signs them,
# with the key in the kmod-key build secret (a PEM holding key and
# certificate), and the certificate where the installer looks for it.
ARG BASE=localhost/stornas-os:console
FROM ${BASE} AS os
FROM quay.io/centos/centos:stream10 AS sign
RUN dnf -y install kernel-devel openssl && dnf clean all
COPY --from=os /usr/lib/modules/ /modules/
RUN --mount=type=secret,id=kmod-key \
    set -e; mkdir /signed; \
    openssl x509 -in /run/secrets/kmod-key -outform DER -out /signed/mok.der; \
    for m in /modules/*/extra/drbd/*.ko; do \
      cp "$m" /signed/; \
      /usr/src/kernels/*/scripts/sign-file sha256 /run/secrets/kmod-key /run/secrets/kmod-key "/signed/${m##*/}"; \
    done
FROM ${BASE}
COPY --from=sign /signed/ /tmp/signed/
RUN kver="$(ls /usr/lib/modules)" && \
    install -m 644 /tmp/signed/*.ko "/usr/lib/modules/${kver}/extra/drbd/" && \
    install -D -m 644 /tmp/signed/mok.der /usr/lib/microshift-installer/mok.der && \
    rm -rf /tmp/signed
LABEL io.github.epheo.installer.secure-boot=on
