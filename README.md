# amorce

Boot media and console wizard that install any image built FROM ghcr.io/epheo/microshift.
Status: in VMs, attended installs on styled and normal consoles, unattended installs from a site file, every install reporting to the first machine's console, and workers joining its cluster.
Screen mockups: the "Installer TUI UX" canvas.

## Use

- One ISO, `amorce.iso`, holds every purpose's image: boot it and pick what to install, with no network needed.
- Planned: published at https://dotvirt.io/amorce.iso; until then, build it (Build media, below).
- Boot it (UEFI) from a USB stick or BMC virtual media; the wizard starts on the screen and on the serial console.
- Keys are listed on every screen; F12 opens a shell; alt+F2 has another.
- The boot menu has a second entry with the normal console look.
- `amorce demo` runs the wizard on a fake machine in any terminal.

Many machines: every install keeps its answers in `/etc/amorce/site.yaml`.
Add machines to that file, then make small site media to boot beside the same ISO:

```
podman run --rm -v $PWD/out:/out:Z -v $PWD:/in:ro,Z localhost/amorce:dev \
  iso --payload-only --site /in/site.yaml --out /out/site.iso
```

- `--site` on the main ISO works too; site media keep that ISO generic.

## Build media

```
make image
mkdir -p out
podman run --rm -v $PWD/out:/out:Z localhost/amorce:dev iso --out /out/amorce.iso \
  --image ghcr.io/epheo/microshift:latest --image ghcr.io/epheo/stornas-os:latest
```

- Every image goes in one OCI layout on the media, so layers they share are stored once.
- Images built from the same distro build share almost all of it.

## Goal

- Zero to running from the machine's own console: monitor, BMC remote console, or serial.
- No laptop, DHCP, or switch change needed to start.
- One generic ISO for every machine and every purpose.
- The first install saves a site file; the same file installs the other machines unattended.
- After install, the console keeps showing what an admin needs: address, fingerprint, first login, health.

## Purposes

- MicroShift: ghcr.io/epheo/microshift; MicroShift, r-drbd, portail. Labels on the distro's branch installer/purpose.
- Storage: ghcr.io/epheo/stornas-os; shares, iSCSI, replication. Labels on stornas's branch installer-plan.
- Virtualization: the dotvirt appliance; KubeVirt, CDI, Argo CD, dotvirt, a git forge, Kanidm if chosen; it needs a domain.
- Planned: every appliance image lives in this repo, under `appliances/`: the dotvirt appliance (written by dotvirt's maintainers) and stornas-os, which moves here from stornas.
- Each image describes itself in labels; the installer hard-codes no purpose.
- Each purpose works without the installer: the installer only feeds its own mechanisms (the distro's below).
- An image without labels can be described by `iso` flags (`--name`, `--summary`, `--data-disks`...).

Labels, prefix `io.github.epheo.installer.`:

```
id          storage
name        Storage
summary     File shares, iSCSI disks, replication
includes    Web UI on port 30080;SMB and NFS shares;...   (';' separated)
data-disks  pairs | thin | none
target      ghcr.io/epheo/stornas-os:latest                (updates, for local builds)
secure-boot off                                            (cannot run with Secure Boot on)
domain      console;git                                    (names it serves under the site's domain, which it then needs)
identity    kanidm:idm                                     (a sign-in provider it can install, and its name)
settings    /etc/dotvirt/appliance.env                     (where the names and sign-in go; under /etc)
reserved-users admin;anonymous                             (names its accounts or its web pages already use)
```

- An image built FROM another inherits its labels: the distro sets `secure-boot=off` only while its DRBD module is unsigned, so derived images get the right one.

- Updates come from where `iso` pulled the image; `target` names that place only for local builds and digest pins.
- Why: labels are inherited, so an image built FROM stornas-os would otherwise update back to stornas-os.
- `iso` prints each image's name and update source.
- `iso` refuses an image whose `domain`, `identity` and `settings` labels disagree: an identity needs names, names need a settings file.

## Non-goals for v1

- A mirrored system disk: `bootc install to-disk` writes one disk.
- Building data pools: each purpose owns its data disks.
- A web UI in the installer: it fails without DHCP, a routed VLAN, or a laptop on site.
- Network boot; BIOS boot.

## Who expects what

- VMware admins: boot media, a wizard, then a console screen like ESXi's.
- OpenShift admins: a config file and hands-off installs.
- A custom ISO per machine does not scale; the site file replaces it.

## Architecture

One image, two jobs:

- Booted from the ISO, it is the live system that runs the wizard.
- Run as a container, it writes that ISO: `amorce iso` squashes its own root and adds the images.
- No repo builds or hosts ISOs; anyone with podman makes media for any bootc image, including their own derived ones.
- No release asset limit applies: stornas alone is 2.3 GB.

Live system:

- FROM quay.io/centos-bootc/centos-bootc:stream10, plus dracut-live, smartmontools and the ISO tools.
- Boots from squashfs with SELinux permissive: bootc needs SELinux on, and the live root carries no labels.
- Masks updates, sshd and the gettys of the wizard's consoles; other VTs autologin root.

Wizard:

- Engine: in process; owns state, tests, and every write; frontends render its snapshots.
- One Bubble Tea program per console (tty1 and the kernel's serial consoles), all in one process.
- Consoles share every answer and the current step; each keeps its own cursor.
- Install: `bootc install to-disk --source-imgref oci:<media> --target-imgref <registry>`; updates come from the registry.
- The media's `console=` arguments become kernel arguments of the installed system.
- Then the wizard writes the new /etc: NetworkManager profiles, hostname, keymap, the data plan.
- New files get labels from the installed policy (`setfiles -c`): it knows types the live policy lacks.
- The install log is kept in the installed `/var/log/amorce.log`.

## Media

- ISO labeled AMORCE; UEFI with CentOS's signed shim, grub and kernel, so it boots with Secure Boot on.
- `images/<name>`: one OCI layout per purpose; `images/purposes.json` holds their descriptions, read from labels when the ISO is written.
- Media labeled AMORCE-* are read too, so images can sit beside the installer (`iso --payload-only`).
- Progress during the copy is bytes read from the media against the image's compressed size.
- At scale: Redfish mounts the same ISO on every BMC; the site file holds each machine's answers.

## Flow

Attended, first machine:

1. Hello: install or rescue shell; keyboard layout, searchable.
2. Network: cable map with LLDP; live tests; the fix pre-selected.
3. Purpose: list from image labels; best-fit badge from the hardware found; or join a first machine heard on the network.
4. Disks: one system disk; the purpose's data plan; spare disks left free; or roles by hand.
5. Name: the hostname, checked as typed; for an image that serves names, the domain and sign-in too.
6. Confirm: every disk erased and every disk kept; typing the hostname unlocks erase.
7. Installing: check, wipe, copy the image, write network, hand over the data plan, add the Secure Boot key.
8. Ready: what the next start does; restarts by itself after 10 seconds unless a key is pressed.

Unattended, when a site file names the machine:

- Hello says which machine this is, checks every answer in wizard order, then counts down 10 s; any key on any console stops it.
- The first answer missing or failing opens the wizard on that screen, with the reason; every answer before it stays filled in.
- It stops at Confirm before erasing a disk that holds something, unless the file says `erase: any`, and when Secure Boot needs a decision.
- A site file that does not name the machine is said on Hello; the serial number is on Hello's panel, to add it.

## Reporting

- The site file's `report: {address, token}` names the first machine.
- An attended install with no site file names itself, with a new token, in the site file it keeps; machines installed from that file report to it.
- Every installer reports: waiting (and why: a screen a person is on, a site answer that failed), installing (step, progress), failed (the error), installed.
- The installed console then reports running, with its address, every 10 minutes; a worker reports joining first.
- The first machine's console lists every machine the site file names, seen or not; F3 shows the whole list.
- Reports go one way: the first machine never sends commands to installers.
- Reports never block an install; a send that fails is retried at the next change.
- TLS on port 9190; the token never crosses the network (see Trust).
- `amorce machines` prints the board as JSON, on any machine holding the site file.

## Join

The distro has two profiles: controller, the default, runs its own cluster; worker joins one.

- A site file's `profile: worker` makes a machine a worker of the first machine's cluster.
- Without a site file, Purpose lists the first machines heard on the network, and "Join by address".
- Joining one asks for the six words its console shows after F4; the first machine answers with its site file.
- The worker then installs the image the first machine runs, with the next free name (store-01 gives store-02).
- The installer masks MicroShift on a worker: started before the join, it would make the machine a controller.
- At its first start, the worker's console asks the first machine for a join kubeconfig, valid 10 minutes.
- Then it runs the distro's steps: `microshift-profile worker`, unmask and enable MicroShift, `microshift add-node --worker`.
- The kubeconfig lives in /run for that one command; add-node removes its own copy.
- It tries again every 30 seconds until the first machine's MicroShift can let it in, so all machines can install at once.
- The first machine adds each worker to /etc/hosts: the apiserver reaches kubelets by node name, and a site may have no DNS.
- Every install pins `node.hostnameOverride`, and `node.nodeIP` for a fixed address, in `/etc/microshift/config.d/10-installer.yaml`.
- First machines broadcast a beacon every 5 seconds on UDP 9190 (site, name, address, purpose, words shown); it is a hint, never trusted.


## Installed console

The distro ships this binary; `amorce console` takes tty1 of every installed machine.

- This machine: address, health (systemd's), serial number, the image updates come from.
- What the purpose tells its admins: every `issue.d/*.issue` file, wrapped, never cut; stornas puts its web address, fingerprint and first password there.
- On the first machine, the board of the site's machines.
- Enter starts a login on tty1; alt+F2 has a plain one; the serial console keeps its getty.
- F2, settings: the network and its time servers, a root password, restart, shut down; the domain and sign-in, read only.
- A new address rewrites what the installer wrote (the NetworkManager profile, MicroShift's `node.nodeIP`, this machine's site file entry), then restarts: configure-ovs builds br-ex from the profile at every start.
- Only a machine nobody else reaches can change its address: not a worker, and not a first machine others report to.
- The console asks no password for its settings: whoever has the console has the machine, as with the installer.
- `ssh -p 2222 root@<machine>`: the same console, its board, words and settings, for root's SSH keys (Wish, in the console's process).
- Kernel messages for the screen go to tty12 (alt+F12, as on ESXi), so they never scroll the console; serial consoles still get them.
- It never waits on a command: systemd is asked every 2 seconds in the background, bootc once per boot.
- Styled when the font loads, normal otherwise, like the wizard.

Distro, in its Containerfile:

```
FROM ghcr.io/epheo/amorce:<tag> AS installer
...
COPY --from=installer /usr/bin/amorce /usr/bin/amorce
COPY --from=installer /usr/share/amorce /usr/share/amorce
RUN install -m 644 /usr/share/amorce/dist/amorce-console.service /usr/lib/systemd/system/ && \
    install -m 644 /usr/share/amorce/dist/amorce.xml /usr/lib/firewalld/services/ && \
    firewall-offline-cmd --zone=public --add-service=amorce && \
    systemctl enable amorce-console.service && \
    systemctl mask getty@tty1.service
```

- `test/vm/console.Containerfile` adds the same lines to any OS image, for tests before the distro has them.
- Why the distro: one binary, updated by bootc with everything else; no repo-specific copy to drift.

## Network

- The live system takes DHCP where it can; the wizard offers to keep that address as a fixed one.
- Settings by hand: interface, VLAN, address, gateway, DNS, time servers (NTP); applied to the live system and tested.
- Tests: carrier, address conflict (ARP), gateway (ARP), DNS (a query for localhost; any answer counts), NTP (one query, the clock left alone).
- Time servers replace the image's public pool, which an air-gapped site never reaches: chrony.conf's `pool` lines are commented out and point at `/etc/amorce/chrony`. Without any, the image's own stay, and DHCP's.
- bootc then keeps the edited chrony.conf: the image's later changes to it no longer apply. Intended: the site's servers stay.
- Gateway probe: when the gateway is silent, ARP for it untagged and on each candidate VLAN; the fix offered is where it answers.
- Candidate VLANs: the port VLAN from LLDP, then 802.1Q tags seen in a few seconds of capture.
- No evidence: the screen says the gateway answers nowhere it looked, and offers to edit or keep.
- LLDP listens on every NIC for the whole session; switches send every 30 seconds.
- Output: NetworkManager keyfiles; a VLAN gets a parent profile with no address, so the parent never tries DHCP.

## Disks

- System: one disk; the smallest healthy SSD of 32 GB or more.
- Data: the purpose's `data-disks` label says what it does with the rest.
- `pairs` (stornas) mirrors same-size disks of the same kind; the operator makes a StoragePool of each pair.
- `thin` (the distro) puts every healthy data disk in VG `myvg1` with thin pool `thin`: the distro's own recipe for its default r-drbd Pool, run by its `microshift-r-drbd-data-disks` unit at the first start.
- An odd disk, and failing ones, stay unused.
- A disk with no `/dev/disk/by-id` name is never data: its kernel name may be another blank disk at the first start.
- The installer wipes the confirmed disks and records the plan; it builds nothing.
- Disks left out stay unclaimed; the purpose's UI can use them later.
- Why: the stornas operator owns node state; the installer must not duplicate it.

Plan file, `/etc/amorce/data-disks.json`:

```json
{"version": 1, "layout": "pairs", "pools": [
  {"name": "hdd-1", "raid": "raid1", "tier": "hdd",
   "devices": ["/dev/disk/by-id/wwn-...", "/dev/disk/by-id/wwn-..."]}]}
```

- `layout` is the image's `data-disks`: each purpose builds only its own layout, so an image switched to another never takes disks in use.

- stornas (branch installer-plan): the agent reports the file in its NodeInventory; the operator creates those StoragePools once and marks the inventory.
- A pool the user deletes later is never recreated.

## Names and sign-in

For an image labeled `domain`, the Name step also asks:

- Domain: the site's DNS points the names under it at this machine, by a wildcard or one record each.
- Required: browsers send an address's cookies to all its ports, so every web app on one address receives the others' sign-in.
- Sign-in, for an image labeled `identity`: a token from the console, or the image's provider.
- The provider needs a first admin: a name, a full name (optional), an email.
- Leaving the domain asks DNS for every name the image serves there (`console.<domain>`, ...).
- A name that does not point at the machine's address warns, on Name and on Confirm; it never blocks: the site may set DNS later.
- A domain on an address left to DHCP warns too: a new lease breaks the names until DNS follows.
- The admin name is 2 to 32 of a to z and 0 to 9, a letter first, and none of the image's `reserved-users`.
- A worker skips it: it serves the first machine's names.
- Domain and sign-in are set at install: passkeys and accounts are bound to them.

The checks, which the image may repeat at its start:

- Domain: lowercase ASCII; an international one in its xn-- form.
- Two labels or more, each 1 to 63 of a to z, 0 to 9 and inner dashes; the last one not all digits.
- No address, no `*.`, no final dot.
- Short enough that its longest name, the provider's included, fits in 253.
- Email: one `@`; before it 1 to 64 ASCII characters, none of space, quotes, `\`, `$`, backticks, `<>()[],;:`.
- After it a domain by the label rules above, in any case; 254 at most in all.
- Full name: 64 characters at most, all printable.

The answers go to the image's `settings` file, mode 0644, no secrets:

```
DOMAIN=virt.example.com
IDENTITY=kanidm
ADMIN_USER=alice
ADMIN_DISPLAY=Alice Martin
ADMIN_EMAIL=alice@example.com
```

- One KEY=value per line, split at the first `=`, the value verbatim: no quotes, escapes or comments.
- `IDENTITY=none` and no `ADMIN_` lines for token sign-in.
- `DOMAIN` is never empty.
- No file: a worker, or a system the installer did not install; the dotvirt appliance sets nothing up until the file exists.
- `ADMIN_DISPLAY` is left out when empty.
- UTF-8; every line ends in LF, the last one too.
- A worker gets no file.
- Display names refuse quotes, `\`, `$` and backticks, so a shell or a unit reads the same value.
- The image ships no file there, so an update never merges over it; it checks the file at every start.

## Trust

- The site file's token is the site's secret: it posts reports and joins workers. Installs keep the file mode 600.
- The first machine makes a TLS key at its first start; nobody checks its certificate against anything.
- Instead each end proves it holds the secret: an HMAC of the first machine's certificate, one for each direction.
- A machine in the middle shows its own certificate, so a proof made for it is worthless at the first machine, and it cannot make the first machine's.
- Join words are a secret too, for machines without the site file: six from the EFF short list 2.0, about 62 bits.
- They last an hour or five wrong tries; F4 closes them sooner. Three letters of each word are enough to type.
- Words work there because both screens are ours; browsers show hex, so the console shows the UI certificate's SHA-256 as byte pairs.
- stornas already prints address, fingerprint, and first admin password on its login screen.

## Secure Boot

- The distro builds the DRBD module; Secure Boot refuses it unless it is signed by a key the kernel trusts.
- The distro signs it in CI with the project key, and ships the certificate as `/usr/lib/amorce/mok.der` (branch kmod/sign-drbd, waiting for the key).
- When the image ships that certificate and Secure Boot is on, the installer queues it with `mokutil --import` and shows the one-time password on Ready.
- An unattended install puts that password on the first machine's board: a person answers MokManager once, at the machine's screen.
- The kernel trusts an enrolled key for modules only in its `.machine` keyring: a CA certificate that signs certificates and nothing else.
- An image labeled `secure-boot=off` gets a warning on Purpose and Confirm, and a firmware step on Ready; no key is queued for it.
- It still installs: stornas boots and serves its UI with Secure Boot on (VM e2e); replicated volumes need it off.
- stornas drops its `secure-boot=off` label once it builds on a signed distro image.

## Display

One layout, three looks:

- Styled console: tty1 on the monitor and BMC screen.
- Normal console: stock colors and font; the serial console, and tty1 whenever styling fails.
- True color: `amorce demo` in a modern terminal, and the console over SSH.
- The normal console is the baseline: every screen works there.
- Meaning never rests on color alone: every state has a symbol and a word.

Styled console:

- The plain Linux console; no graphics stack.
- The wizard loads its font, sets the 16 colors (PIO_CMAP), and reads both back (GIO_UNIMAP, GIO_CMAP); any miss means the normal look.
- The F12 shell and the console's login get the stock colors back, and the screen its own after.
- A console restarted after a crash finds its own colors still set, and takes the kernel's as stock.
- The console only draws backgrounds from slots 0 to 7, so those hold every background color.
- The console draws bright colors (SGR 90 to 97) as bold normal ones, and bold sticks; every normal color is sent with "normal intensity" (SGR 22).
- No bold: the console turns bold into another color slot.
- Gradients step through Charple, Hazy, Mauve, Dolly.
- `inst.look=normal` forces the normal look; the boot menu has an entry for it.
- Kernel messages stay off the consoles while the wizard runs.

Font:

- 256 glyphs: 512 would cut foreground colors to 8.
- ASCII and Latin-1 sit at their own code points: the kernel clears the screen with slot 32.
- Text comes from Terminus (Lat2-Terminus16, shipped by kbd); box drawing, blocks, check marks and spinner frames are drawn pixel by pixel so lines join.
- 8x16, and 16x32 when the screen still fits 100 columns.

Palette, slot and color:

```
0 Pepper  #201F26    8 Iron    #4D4C57
1 Coral   #FF577D    9 Oyster  #605F6B
2 Julep   #00FFB2   10 Bok     #68FFD6
3 Char    #3A3943   11 Mustard #F5EF34
4 Charple #6B50FF   12 Hazy    #8B75FF
5 Dolly   #FF60FF   13 Mauve   #D46EFF
6 Malibu  #00A4FF   14 Steam   #A2A0AD
7 Sash    #ECEBF0   15 Smoke   #BFBCC8
```

Normal console:

- ASCII stands in for missing symbols: `+` done, `x` failed, `*` current, `-` pending, `|` spinner, `=` cable.
- Square corners; single box lines and blocks stay, since common console fonts have them.
- Pills use the dim background twin of each color.

All consoles:

- Only the cursor moves every VT220 descendant has; the renderer's shortcuts for TERM=linux misplace lines.
- Serial consoles ask the terminal for its size (cursor position report); no answer means 80x24.
- 100x30 and up: the mockups' layout, centered; smaller: no side panel, spare blank lines dropped.

## Site file

```yaml
site: lab
report: {address: 10.20.120.11, token: ...}   # the first machine; the token is the site's secret
defaults:
  purpose: storage                 # an id from the media; optional with one purpose
  keyboard: fr
  network: {interface: eno1, vlan: 110, gateway: 10.20.120.1, dns: [10.20.0.53], ntp: [10.20.0.53]}
  domain: virt.example.com         # required by an image that serves names
  identity: kanidm                 # its sign-in provider, or none for a token
  admin: {user: alice, display: Alice Martin, email: alice@example.com}
  disks: {system: auto, data: auto, erase: blank}
  ssh_authorized_keys: ["ssh-ed25519 AAAA... admin@lab"]   # for root
  profile: worker                  # join the first machine's cluster; controller is the default
machines:
  - match: {serial: CZ2041XY7J}
    name: srv-01
    address: 10.20.120.11/24
    profile: controller            # the first machine
  - match: {serial: CZ2041XY7K}
    name: srv-02
    address: 10.20.120.12/24       # or dhcp
  - match: {mac: "3c:ec:ef:00:00:03"}
    name: srv-03
    address: 10.20.120.13/24
    disks: {system: nvme-SSD0002}  # kernel name, serial or by-id name
```

- One file per site, never one per machine; a machine's fields override the defaults.
- Machines match by serial number (DMI; placeholders like "To Be Filled By O.E.M." are ignored) or any MAC.
- Unknown fields are errors, so a typo never reads as an unanswered question; `iso --site` checks the file first.
- `disks.data`: auto (the purpose's plan) or none. `disks.erase`: blank (default) or any.
- Read from any install medium's root: the ISO, or media labeled AMORCE-*.
- Every install writes one for its own machine, shared settings under defaults.
- `ssh_authorized_keys` go to root through bootc; the wizard has no screen for them.
- A worker needs `report`; the first machine cannot be one.
- `domain`, `identity` and `admin` answer Names and sign-in; an image that serves no names refuses them.
- Planned: an admin password hash.

## Testing

- `make test`: plan proposals, LLDP parsing, NetworkManager profiles, the font, site files and unattended runs on the demo machine, and every screen fitting 100x30 and 80x24 in every look.
- `amorce screens DIR`: every demo screen as terminal output, to compare with the mockups.
- `test/render`: turns that output into PNGs through a terminal emulator.
- `test/vm/e2e.sh VM PORT`: a fresh libvirt VM boots the ISO; the wizard is driven over the serial console; the installed system must serve its web UI.
- `SB=on test/vm/e2e.sh VM PORT`: the same with Secure Boot enforced; the warnings must show.
- `test/vm/edge.sh VM PORT`: a PERC RAID volume and a network with no DHCP and no gateway.
- `test/vm/site.sh VM PORT`: site media name the VM by serial; no key is pressed until the install is done; the installed system keeps the answers.
- `test/vm/media.sh BASE`: the installer ISO, and a payload of the OS image BASE with the installed console added as the distro adds it (`console.Containerfile`).
- `test/vm/fleet.sh PREFIX PORT`: two VMs of one site; the second reports installing, installed, then running to the first one's console.
- `KMOD_KEY=key.pem PAYLOAD=msi-signed.iso test/vm/media.sh BASE`, then `PAYLOAD=msi-signed.iso test/vm/mok.sh VM PORT`: an image with DRBD signed by a test key, the way the distro signs it (`signed.Containerfile`); Secure Boot on; an unattended install queues the key; the test answers MokManager over the serial console; DRBD must load under lockdown.
- `test/vm/settings.sh VM PORT`: over SSH to an installed console, a root password and a new address; the machine restarts on it, MicroShift and the UI follow, the password logs in, and F2 opens the settings on tty1. `ptydrive.py` drives the SSH session.
- `test/vm/purpose.sh VM PORT`: media with MicroShift beside Storage; a site file picks MicroShift; the distro builds the data disks into its default Pool. Until the distro's branch installer/purpose builds, `purpose.Containerfile` adds its unit and labels to the published image.
- `test/vm/virt.sh VM PORT`: the Virtualization image installed unattended with no registry in reach; KubeVirt, CDI and dotvirt come up from embedded images; the console answers HTTPS on the machine's address with the certificate its screen shows; a token signs the first admin in.
- `PAYLOAD=msi-names-payload.iso test/vm/media.sh localhost/microshift:names`, then `test/vm/names.sh VM PORT`: an image labeled as the dotvirt appliance is (`names.Containerfile`); a site file gives it a domain, Kanidm and its first admin, and a time server; the test network's DNS points `*.virt.test` at the machine but `idm.virt.test` elsewhere. The installed system must hold the settings file byte for byte, chrony on the site's server, and in its log what DNS said; F2 must show the domain and sign-in, and a LoadBalancer Service must follow the machine to a new address.
- `test/vm/join.sh PREFIX PORT`: three VMs, one cluster. The site file installs the first machine and a worker side by side; a third machine without the site file joins with the words read off the first machine's screen; all three nodes must be Ready.
- `test/vm/pools.sh VM`: the pools the installed stornas built from the plan, through its API.
- `test/vm/vm.sh shot VM FILE`: the VM's screen, to check the styled console.

## Failure cases

Designed during implementation, not in the mockups:

- Hardware RAID volumes (handled): known controller models are marked RAID; a purpose that mirrors drives itself says to set the controller to HBA mode.
- Odd or mixed data disk counts (handled: odd ones stay unused).
- A disk with no stable name (handled): it can hold the system, never data; the disks screen says so.
- Old md, LVM, or filesystem signatures on chosen disks (handled: shown, then wiped).
- No DHCP and no gateway (handled): the gateway is optional; the result says only the subnet is in reach. stornas serves its UI without a default route.
- Secure Boot on for an image that needs it off (handled): warned on Purpose, Confirm and Ready; installs anyway.
- Bonds that need LACP before any link comes up.
- No LLDP and no tagged traffic (handled: the gateway probe still tries untagged).

## Phases

1. Single machine, attended, one system disk; LLDP and gateway probe; styled and normal console; serial e2e. Done, in VMs.
2. Site file, unattended installs, reporting to the first machine's installed console: done, in VMs.
3. Join: workers from the site file and by words, discovery: done, in VMs.
4. Secure Boot enrollment: done, in VMs with a test key; the distro's signing waits for the project key.
5. Installed console: the screen, the board, settings (F2), and SSH through Wish: done, in VMs.

## Dependencies

- Join words: the EFF short word list 2.0 (Electronic Frontier Foundation, CC BY 3.0 US).

- stornas: the install-plan branch, and the labels in its image.
- distro: the installed console lines above, once this repo publishes its image; branch installer/purpose (labels, the data disks unit); branch kmod/sign-drbd and its KMOD_SIGNING_KEY secret.
- dotvirt: branches operator/hostless-gateway and image/virtualization; portail: branch gateway/fixed-nodeport, then a release the distro embeds.
- Registry credentials for updates after an offline install.

## Open questions

- Bonds: offered when two links reach different switches, or always asked?
- Unattended installs with Secure Boot on: MOK needs a person per machine; check BMC (Redfish) enrollment into db, which the kernel puts in .platform, not .machine.
- A 16x32 font drawn for that size rather than doubled.
- The site token never expires; rotating it means a new site file for every machine.
- Settings at the console need no login; a site that wants one could ask for it in the site file.

## License

Apache License 2.0: see [LICENSE](LICENSE).
