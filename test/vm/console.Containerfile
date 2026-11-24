# An OS image with the installed console, as the distro adds it; the RUN
# below is the distro's part, word for word.
ARG INSTALLER=localhost/microshift-installer:dev
ARG BASE=localhost/stornas-os:plan
FROM ${INSTALLER} AS installer
FROM ${BASE}
COPY --from=installer /usr/bin/installer /usr/bin/installer
COPY --from=installer /usr/share/microshift-installer /usr/share/microshift-installer
RUN install -m 644 /usr/share/microshift-installer/dist/installer-console.service /usr/lib/systemd/system/ && \
    install -m 644 /usr/share/microshift-installer/dist/microshift-installer.xml /usr/lib/firewalld/services/ && \
    firewall-offline-cmd --zone=public --add-service=microshift-installer && \
    systemctl enable installer-console.service && \
    systemctl mask getty@tty1.service
