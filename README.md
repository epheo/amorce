# microshift-installer

Boot media and console wizard that install any image built FROM ghcr.io/epheo/microshift.
Status: in VMs, attended installs on styled and normal consoles, unattended installs from a site file, and every install reporting to the first machine's console.
Screen mockups: the "Installer TUI UX" canvas.

## Use

```
make image
mkdir -p out
podman run --rm -v $PWD/out:/out:Z localhost/microshift-installer:dev \
  iso --out /out/stornas.iso --image ghcr.io/epheo/stornas-os:latest
```

- Boot the ISO (UEFI) from a CD, USB stick or BMC virtual media; the wizard starts on the screen and on the serial console.
- Keys are listed on every screen; F12 opens a shell; alt+F2 has another.
- The boot menu has a second entry with the normal console look.
- `installer demo` runs the wizard on a fake machine in any terminal.

Many machines: every install keeps its answers in `/etc/microshift-installer/site.yaml`.
Add machines to that file, then make small site media to boot beside the same ISO:

```
podman run --rm -v $PWD/out:/out:Z -v $PWD:/in:ro,Z localhost/microshift-installer:dev \
  iso --payload-only --site /in/site.yaml --out /out/site.iso
```

- `--site` on the main ISO works too; site media keep that ISO generic.

## Goal

- Zero to running from the machine's own console: monitor, BMC remote console, or serial.
- No laptop, DHCP, or switch change needed to start.
- One generic ISO for every machine and every purpose.
- The first install saves a site file; the same file installs the other machines unattended.
- After install, the console keeps showing what an admin needs: address, fingerprint, first login, health.

## Purposes

- MicroShift: ghcr.io/epheo/microshift; MicroShift, r-drbd, portail.
- Storage: ghcr.io/epheo/stornas-os; shares, iSCSI, replication.
- Virtualization: the da2 image; VMs, kanidm, deskube, forgejo.
- Each image describes itself in labels; the installer hard-codes no purpose.
- An image without labels can be described by `iso` flags (`--name`, `--summary`, `--data-disks`...).

Labels, prefix `io.github.epheo.installer.`:

```
id          storage
name        Storage
summary     File shares, iSCSI disks, replication
includes    Web UI on port 30080;SMB and NFS shares;...   (';' separated)
data-disks  pairs | none
target      ghcr.io/epheo/stornas-os:latest                (updates, for local builds)
secure-boot off                                            (cannot run with Secure Boot on)
```

- Updates come from where `iso` pulled the image; `target` names that place only for local builds and digest pins.
- Why: labels are inherited, so an image built FROM stornas-os would otherwise update back to stornas-os.
- `iso` prints each image's name and update source.

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
- Run as a container, it writes that ISO: `installer iso` squashes its own root and adds the images.
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
- The install log is kept in the installed `/var/log/microshift-installer.log`.

## Media

- ISO labeled MSINST; UEFI with CentOS's signed shim, grub and kernel, so it boots with Secure Boot on.
- `images/<name>`: one OCI layout per purpose; `images/purposes.json` holds their descriptions, read from labels when the ISO is written.
- Media labeled MSPAYLOAD* are read too, so images can sit beside the installer (`iso --payload-only`).
- Progress during the copy is bytes read from the media against the image's compressed size.
- At scale: Redfish mounts the same ISO on every BMC; the site file holds each machine's answers.

## Flow

Attended, first machine:

1. Hello: install or rescue shell; keyboard layout, searchable.
2. Network: cable map with LLDP; live tests; the fix pre-selected.
3. Purpose: list from image labels; best-fit badge from the hardware found.
4. Disks: one system disk; the purpose's data plan; spare disks left free; or roles by hand.
5. Name: the hostname, checked as typed.
6. Confirm: every disk erased and every disk kept; typing the hostname unlocks erase.
7. Installing: check, wipe, copy the image, write network, hand over the data plan, add the Secure Boot key.
8. Ready: what the next start does; restarts by itself after 10 seconds unless a key is pressed.

Unattended, when a site file names the machine:

- Hello says which machine this is, checks every answer in wizard order, then counts down 10 s; any key on any console stops it.
- The first answer missing or failing opens the wizard on that screen, with the reason; every answer before it stays filled in.
- It stops at Confirm before erasing a disk that holds something, unless the file says `erase: any`, and when Secure Boot needs a decision.
- A site file that does not name the machine is said on Hello; the serial number is on Hello's panel, to add it.

Planned:

- Join: pick a discovered cluster or type its address; compare six words with its console.

## Reporting

- The site file's `report: {address, token}` names the first machine.
- An attended install with no site file names itself, with a new token, in the site file it keeps; machines installed from that file report to it.
- Every installer reports: waiting (and why: a screen a person is on, a site answer that failed), installing (step, progress), failed (the error), installed.
- The installed console then reports running, with its address, every 10 minutes.
- The first machine's console lists every machine the site file names, seen or not; F3 shows the whole list.
- Reports go one way: the first machine never sends commands to installers.
- Reports never block an install; a send that fails is retried at the next change.
- HTTP on port 9190 with the token; reports hold names, addresses and progress, nothing secret.
- Planned: the MOK password on the board, for a machine that needs a person; Join.

## Installed console

The distro ships this binary; `installer console` takes tty1 of every installed machine.

- This machine: address, health (systemd's), serial number, the image updates come from.
- What the purpose tells its admins: every `issue.d/*.issue` file, wrapped, never cut; stornas puts its web address, fingerprint and first password there.
- On the first machine, the board of the site's machines.
- Enter starts a login on tty1; alt+F2 has a plain one; the serial console keeps its getty.
- Kernel messages for the screen go to tty12 (alt+F12, as on ESXi), so they never scroll the console; serial consoles still get them.
- It never waits on a command: systemd is asked every 2 seconds in the background, bootc once per boot.
- Styled when the font loads, normal otherwise, like the wizard.

Distro, in its Containerfile:

```
FROM ghcr.io/epheo/microshift-installer:<tag> AS installer
...
COPY --from=installer /usr/bin/installer /usr/bin/installer
COPY --from=installer /usr/share/microshift-installer /usr/share/microshift-installer
RUN install -m 644 /usr/share/microshift-installer/dist/installer-console.service /usr/lib/systemd/system/ && \
    install -m 644 /usr/share/microshift-installer/dist/microshift-installer.xml /usr/lib/firewalld/services/ && \
    firewall-offline-cmd --zone=public --add-service=microshift-installer && \
    systemctl enable installer-console.service && \
    systemctl mask getty@tty1.service
```

- `test/vm/console.Containerfile` adds the same lines to any OS image, for tests before the distro has them.
- Why the distro: one binary, updated by bootc with everything else; no repo-specific copy to drift.

## Network

- The live system takes DHCP where it can; the wizard offers to keep that address as a fixed one.
- Settings by hand: interface, VLAN, address, gateway, DNS; applied to the live system and tested.
- Tests: carrier, address conflict (ARP), gateway (ARP), DNS (a query for localhost; any answer counts).
- Gateway probe: when the gateway is silent, ARP for it untagged and on each candidate VLAN; the fix offered is where it answers.
- Candidate VLANs: the port VLAN from LLDP, then 802.1Q tags seen in a few seconds of capture.
- No evidence: the screen says the gateway answers nowhere it looked, and offers to edit or keep.
- LLDP listens on every NIC for the whole session; switches send every 30 seconds.
- Output: NetworkManager keyfiles; a VLAN gets a parent profile with no address, so the parent never tries DHCP.

## Disks

- System: one disk; the smallest healthy SSD of 32 GB or more.
- Data: the purpose's `data-disks` label says what it does with the rest; `pairs` mirrors same-size disks of the same kind.
- An odd disk, and failing ones, stay unused.
- The installer wipes the confirmed disks and records the plan; it builds nothing.
- Disks left out stay unclaimed; the purpose's UI can use them later.
- Why: the stornas operator owns node state; the installer must not duplicate it.

Plan file, `/etc/microshift-installer/data-disks.json`:

```json
{"version": 1, "pools": [
  {"name": "hdd-1", "raid": "raid1", "tier": "hdd",
   "devices": ["/dev/disk/by-id/wwn-...", "/dev/disk/by-id/wwn-..."]}]}
```

- stornas (branch installer-plan): the agent reports the file in its NodeInventory; the operator creates those StoragePools once and marks the inventory.
- A pool the user deletes later is never recreated.

## Trust (planned)

- Join: six words from the first machine's join key fingerprint (BIP39-style list).
- Words work there because both screens are ours.
- Browsers show hex, so the console shows the UI certificate's SHA-256 as byte pairs, like browsers do.
- stornas already prints address, fingerprint, and first admin password on its login screen.
- Join codes are short, expire, and bind to the cluster that issued them.
- The site file holds no plaintext secret: password hashes, and a join token that expires and only admits the listed serials.

## Secure Boot

- The DRBD module is unsigned today; Secure Boot refuses it.
- Plan: the distro signs the module in CI with a project key; the public cert ships in the image.
- When the image ships `/usr/lib/microshift-installer/mok.der` and Secure Boot is on, the installer queues it with `mokutil --import` and shows the one-time password on Ready.
- At the next boot, MokManager asks for that password once.
- An image labeled `secure-boot=off` gets a warning on Purpose and Confirm, and a firmware step on Ready; no key is queued for it.
- It still installs: stornas boots and serves its UI with Secure Boot on (VM e2e); replicated volumes need it off.

## Display

One layout, three looks:

- Styled console: tty1 on the monitor and BMC screen.
- Normal console: stock colors and font; the serial console, and tty1 whenever styling fails.
- True color: `installer demo` in a modern terminal; SSH later.
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
defaults:
  purpose: storage                 # an id from the media; optional with one purpose
  keyboard: fr
  network: {interface: eno1, vlan: 110, gateway: 10.20.120.1, dns: [10.20.0.53]}
  disks: {system: auto, data: auto, erase: blank}
  ssh_authorized_keys: ["ssh-ed25519 AAAA... admin@lab"]   # for root
machines:
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
- Read from any install medium's root: the ISO, or media labeled MSPAYLOAD*.
- Every install writes one for its own machine, shared settings under defaults.
- `ssh_authorized_keys` go to root through bootc; the wizard has no screen for them.
- Planned: an admin password hash, NTP, join.

## Testing

- `make test`: plan proposals, LLDP parsing, NetworkManager profiles, the font, site files and unattended runs on the demo machine, and every screen fitting 100x30 and 80x24 in every look.
- `installer screens DIR`: every demo screen as terminal output, to compare with the mockups.
- `test/render`: turns that output into PNGs through a terminal emulator.
- `test/vm/e2e.sh VM PORT`: a fresh libvirt VM boots the ISO; the wizard is driven over the serial console; the installed system must serve its web UI.
- `SB=on test/vm/e2e.sh VM PORT`: the same with Secure Boot enforced; the warnings must show.
- `test/vm/edge.sh VM PORT`: a PERC RAID volume and a network with no DHCP and no gateway.
- `test/vm/site.sh VM PORT`: site media name the VM by serial; no key is pressed until the install is done; the installed system keeps the answers.
- `test/vm/fleet.sh PREFIX PORT`: two VMs of one site; the second reports installing, installed, then running to the first one's console. Needs a payload built from `console.Containerfile`.
- `test/vm/pools.sh VM`: the pools the installed stornas built from the plan, through its API.
- `test/vm/vm.sh shot VM FILE`: the VM's screen, to check the styled console.

## Failure cases

Designed during implementation, not in the mockups:

- Hardware RAID volumes (handled): known controller models are marked RAID; a purpose that mirrors drives itself says to set the controller to HBA mode.
- Odd or mixed data disk counts (handled: odd ones stay unused).
- Old md, LVM, or filesystem signatures on chosen disks (handled: shown, then wiped).
- No DHCP and no gateway (handled): the gateway is optional; the result says only the subnet is in reach. stornas serves its UI without a default route.
- Secure Boot on for an image that needs it off (handled): warned on Purpose, Confirm and Ready; installs anyway.
- Bonds that need LACP before any link comes up.
- No LLDP and no tagged traffic (handled: the gateway probe still tries untagged).

## Phases

1. Single machine, attended, one system disk; LLDP and gateway probe; styled and normal console; serial e2e. Done, in VMs.
2. Site file, unattended installs, reporting to the first machine's installed console: done, in VMs.
3. Join: join service on the first machine, discovery, words.
4. Secure Boot enrollment, after distro module signing.
5. Installed console: the screen and board are done; next, changing settings from it (network, like ESXi's F2), and an SSH frontend through Wish.

## Dependencies

- stornas: the install-plan branch, and the labels in its image.
- distro: the installed console lines above, once this repo publishes its image; labels for the MicroShift purpose (safe now: updates follow the pull, not inherited labels); DRBD module signing (phase 4).
- da2: labels; registry credentials for updates after an offline install.

## Open questions

- Bonds: offered when two links reach different switches, or always asked?
- Unattended installs with Secure Boot on: MOK needs a person per machine; check BMC (Redfish) key enrollment, and whether the kernel trusts those keys for modules.
- A 16x32 font drawn for that size rather than doubled.
- Join service: where it runs, and how long codes live.
- Repo name.
