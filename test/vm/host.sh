#!/bin/bash
# host.sh: makes a GitHub ubuntu-24.04 runner a vm.sh host. Run as root.
set -euo pipefail
# Room for the images, the media and the VM disks.
rm -rf /usr/local/lib/android /usr/share/dotnet /opt/ghc /opt/hostedtoolcache/CodeQL
# The azure apt mirror can stall for hours: the main archive, every call
# bounded. A file still naming the mirror means the rewrite missed.
src=$(ls /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources \
  /etc/apt/apt-mirrors.txt 2>/dev/null || true)
[ -z "$src" ] || sed -i 's/azure\.archive\.ubuntu\.com/archive.ubuntu.com/g' $src
if [ -n "$src" ] && grep -qs azure.archive.ubuntu.com $src; then echo "apt pin missed" >&2; exit 1; fi
timeout 120 apt-get update -q
DEBIAN_FRONTEND=noninteractive timeout 900 apt-get install -y -q --no-install-recommends \
  libvirt-daemon-system libvirt-clients qemu-system-x86 qemu-utils ovmf virtinst dnsmasq-base python3-pyte
systemctl start libvirtd
export LIBVIRT_DEFAULT_URI=qemu:///system
if ! virsh -q pool-info default >/dev/null 2>&1; then
  virsh -q pool-define-as default dir --target /var/lib/libvirt/images
  virsh -q pool-start default
fi
virsh -q net-info default 2>/dev/null | grep -q 'Active: *yes' || virsh -q net-start default
df -h /
