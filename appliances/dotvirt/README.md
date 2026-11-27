# The dotvirt appliance

A bootc OS image: [epheo/microshift](https://github.com/epheo/microshift)
with KubeVirt, CDI, Argo CD, [dotvirt](https://github.com/epheo/dotvirt) and
Kanidm. One machine is a whole virtualization host with its web console. The
installer offers it as the Virtualization purpose.

## What it runs

- KubeVirt and CDI, one replica of each control component.
- Argo CD, the upstream install: sync impersonation enforced, submodules off, no local admin. People work in dotvirt.
- dotvirt's operator, from its release's `operator.yaml`, and one install with a managed forge, in `dotvirt-operator`.
- portail, the image the base embeds, with one Gateway on the machine's address.
- Kanidm, when the installer's sign-in is Kanidm.
- Every image is embedded and imported at boot by the base: no registry needs to be in reach.
- Data disks: the base's thin pool, its default r-drbd Pool (`data-disks=thin`).

## Names

The appliance needs a domain; the site's DNS points these names at the machine:

- `console.<domain>`: the dotvirt console.
- `git.<domain>`: the forge, where pull requests are reviewed and merged.
- `idm.<domain>`: Kanidm, with Kanidm sign-in.

- One Gateway on MicroShift's LoadBalancer, on the machine's address: every name on :443, picked by SNI; :80 redirects.
- Each web app on its own name: browsers send an address's cookies to all its ports, never a name's to another name.
- Kanidm terminates its own TLS (passthrough): the apiserver checks Kanidm's certificate itself.

## Certificates

- A machine CA, made at the first start, signs every certificate. It may sign only names under the domain (a name constraint).
- Trust it once in each browser; passkeys need it. The screen gives its SHA-256, and `dotvirt-appliance ca` prints it.
- Its certificates last 397 days; a daily timer renews those within 30 days of their end.

## Sign-in

The installer writes `/etc/dotvirt/appliance.env` (its README, Names and sign-in). Without it the appliance sets up no dotvirt.

`IDENTITY=kanidm`:

- The apiserver trusts the ID tokens Kanidm mints for dotvirt; dotvirt hands each person's on as their bearer.
- Kubernetes names them `idm:<name>`, their groups `idm:<group>`; `idm:dotvirt_admins` is cluster-admin.
- The forge signs them in through Kanidm too; `dotvirt_admins` are its administrators.
- The first admin is in `dotvirt_admins`. The screen shows an invite link to set a passkey or a password.
- The link lasts an hour, and only its last opening counts. `dotvirt-appliance invite` makes a new one.
- Everyone else: add them in Kanidm, then grant them RBAC; every person may sign in, and sees what RBAC grants.

`IDENTITY=none`:

- Sign in with a token: `dotvirt-appliance token`, on the machine.
- The console then shows the forge admin's password, in its change tray.

The domain and the sign-in are set at install: passkeys, accounts and certificates are bound to them. A start with another one refuses it.

## On the machine

- `dotvirt-appliance-setup.service`, before MicroShift: certificates, the units naming the domain in `/etc/microshift/manifests.d/dotvirt-*`, the apiserver's trust, the screen.
- `dotvirt-appliance-identity.service`, with Kanidm, after MicroShift: Kanidm's clients and their secrets, `dotvirt_admins`, the first admin.
- `dotvirt-appliance-renew.timer`: certificates near their end.
- State in `/var/lib/dotvirt-appliance`: the CA and its certificates.

## Building

```
appliances/dotvirt/build.sh [TAG]
```

- `versions.env`: the base, portail's image, dotvirt's release and its `operator.yaml` sha256, Kanidm.
- `manifests.sha256`: every upstream manifest, fetched at build time and checked.
- `operands`: the images KubeVirt's operator deploys without a manifest naming them.
- `local/`, not committed: `operator.yaml` from `DOTVIRT_OPERATOR_YAML` when set, and images built here.
- An unreleased dotvirt: `make -C operator --no-print-directory kustomize-build > operator.yaml` in its tree, then `DOTVIRT_OPERATOR_YAML=operator.yaml`.
- `OPERATOR_IMAGE` and `DOTVIRT_IMAGE` replace the release's images; an image built here goes in `local/` as an archive, with a line `<archive> <ref>` in `local/images`.

`test/setup.sh` runs the setup under a scratch root, for both sign-ins, and the settings checks.
