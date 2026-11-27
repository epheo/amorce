# One image, two jobs: booted from the ISO it is the live system that runs
# the wizard; run as a container it writes that ISO (amorce iso).
FROM docker.io/library/golang:1.26.10 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags=-s -o /amorce ./cmd/amorce

FROM quay.io/centos-bootc/centos-bootc:stream10
RUN dnf -y install dracut-live smartmontools squashfs-tools xorriso mtools \
 && dnf clean all && rm -rf /var/cache/dnf /var/log/dnf* /var/lib/dnf
COPY --from=build /amorce /usr/bin/amorce
COPY live/rootfs/ /
# What OS images copy in for the installed console; see README, Distro.
COPY dist/ /usr/share/amorce/dist/
RUN amorce font /usr/share/amorce \
 && systemctl mask bootc-fetch-apply-updates.timer rpm-ostree-countme.timer \
      bootc-publish-rhsm-facts.service NetworkManager-wait-online.service \
      sshd.service chronyd.service bootloader-update.service \
      getty@tty1.service serial-getty@.service \
 && systemctl enable amorce.service \
 && passwd -d root
RUN set -e; kver=$(ls /usr/lib/modules); \
    dracut --force --no-hostonly --add dmsquash-live --omit plymouth \
      --kver "$kver" "/usr/lib/modules/$kver/initramfs-live.img"
ENTRYPOINT ["/usr/bin/amorce"]
