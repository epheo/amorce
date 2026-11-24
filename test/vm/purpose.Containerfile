# The distro's branch installer/purpose on its published image, for tests
# before that branch builds. Build context: the distro's src/r-drbd. target
# stands in for the registry a pulled image comes from.
#   podman build -t localhost/microshift:purpose -f test/vm/purpose.Containerfile <distro>/src/r-drbd
FROM ghcr.io/epheo/microshift:latest
COPY --chmod=755 data-disks.sh /usr/libexec/microshift-r-drbd-data-disks
COPY microshift-r-drbd-data-disks.service /usr/lib/systemd/system/microshift-r-drbd-data-disks.service
RUN systemctl enable microshift-r-drbd-data-disks.service
LABEL io.github.epheo.installer.id="microshift" \
      io.github.epheo.installer.name="MicroShift" \
      io.github.epheo.installer.summary="Kubernetes for your own apps" \
      io.github.epheo.installer.includes="MicroShift on the OKD payload;Replicated volumes (r-drbd);Gateway API (portail);Updates that roll back;Workers that join" \
      io.github.epheo.installer.data-disks="thin" \
      io.github.epheo.installer.secure-boot="off" \
      io.github.epheo.installer.target="ghcr.io/epheo/microshift:latest"
