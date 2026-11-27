# An OS image with the installed console, as the distro adds it; the RUN
# below is the distro's part, word for word.
ARG INSTALLER=localhost/amorce:dev
ARG BASE=localhost/stornas-os:plan
FROM ${INSTALLER} AS installer
FROM ${BASE}
COPY --from=installer /usr/bin/amorce /usr/bin/amorce
COPY --from=installer /usr/share/amorce /usr/share/amorce
RUN install -m 644 /usr/share/amorce/dist/amorce-console.service /usr/lib/systemd/system/ && \
    install -m 644 /usr/share/amorce/dist/amorce.xml /usr/lib/firewalld/services/ && \
    firewall-offline-cmd --zone=public --add-service=amorce && \
    systemctl enable amorce-console.service && \
    systemctl mask getty@tty1.service
