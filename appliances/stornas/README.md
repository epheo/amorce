# stornas-os

A bootc OS image: [epheo/microshift](https://github.com/epheo/microshift) with
[stornas](https://github.com/epheo/stornas), every workload image embedded,
so a machine with no registry in reach comes up whole. The installer offers
it as the Storage purpose.

- This directory is the OS layer: packages, Samba, NFS, firewall, the
  greenboot check, and the installer's labels.
- stornas owns its manifests (`image/manifests/stornas`) and its component
  images; `versions.env` pins a stornas commit and the images its CI
  published for it, by digest.
- Data disks: the installer's plan as mirrored pairs (`data-disks=pairs`),
  which stornas's operator turns into StoragePools.

## Building

```
./build.sh ghcr.io/epheo/stornas-os:<tag>
```

From a stornas checkout, after its `make images`:

```
STORNAS_SRC=../stornas ./build.sh
```
